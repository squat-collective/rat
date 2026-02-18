"use client";

import { useEffect, useRef, useState, useCallback } from "react";
import { useSWRConfig } from "swr";
import { KEYS } from "@/lib/cache-keys";

/** Shape of a single log entry received via SSE `event: log`. */
export interface SSELogEntry {
  timestamp: string;
  level: string;
  message: string;
}

/** Shape of a status event received via SSE `event: status`. */
export interface SSEStatusEvent {
  status: string;
}

/**
 * @deprecated Use SSELogEntry instead. Kept for backward compatibility.
 */
type LogEntry = SSELogEntry;

/** Maximum number of log entries to retain in memory. */
const MAX_LOG_ENTRIES = 10_000;

/** Interval (ms) for flushing buffered log entries to state. */
const FLUSH_INTERVAL_MS = 100;

/** Initial reconnection delay in milliseconds. */
const RECONNECT_INITIAL_DELAY_MS = 1_000;

/** Maximum reconnection delay in milliseconds (caps the exponential growth). */
const RECONNECT_MAX_DELAY_MS = 30_000;

/** Maximum number of consecutive reconnection attempts before giving up. */
const RECONNECT_MAX_ATTEMPTS = 10;

/** Run statuses that indicate the run is finished — no reconnection needed. */
const TERMINAL_STATUSES = new Set(["success", "failed", "cancelled"]);

/**
 * SSE hook for live log streaming from an active pipeline run.
 * Connects to GET /api/v1/runs/{id}/logs with Accept: text/event-stream.
 * Accumulates log entries via `event: log` messages.
 * Detects completion via `event: status` messages.
 * Auto-closes on unmount or when run reaches terminal state.
 *
 * Reconnects automatically on connection failure using exponential backoff:
 * starts at 1s, doubles each attempt up to 30s, gives up after 10 attempts.
 * Resets backoff on successful connection. Stops reconnecting when the
 * component unmounts or the run reaches a terminal status.
 *
 * Uses a ref-based buffer with batched flushes to avoid O(n^2) array
 * copying on every incoming log entry.
 */
export function useRunLogsSSE(runId: string, isActive: boolean) {
  const [logs, setLogs] = useState<LogEntry[]>([]);
  const [status, setStatus] = useState<string | null>(null);
  const eventSourceRef = useRef<EventSource | null>(null);
  const bufferRef = useRef<LogEntry[]>([]);
  const flushTimerRef = useRef<ReturnType<typeof setInterval> | null>(null);
  const reconnectTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const attemptRef = useRef(0);
  const unmountedRef = useRef(false);
  const statusRef = useRef<string | null>(null);
  const { mutate } = useSWRConfig();

  // Keep statusRef in sync so the reconnect logic can read the latest status
  // without triggering effect re-runs.
  useEffect(() => {
    statusRef.current = status;
  }, [status]);

  const flushBuffer = useCallback(() => {
    if (bufferRef.current.length === 0) return;
    const batch = bufferRef.current;
    bufferRef.current = [];
    setLogs((prev) => {
      const merged = prev.concat(batch);
      if (merged.length > MAX_LOG_ENTRIES) {
        return merged.slice(merged.length - MAX_LOG_ENTRIES);
      }
      return merged;
    });
  }, []);

  const clearReconnectTimer = useCallback(() => {
    if (reconnectTimerRef.current) {
      clearTimeout(reconnectTimerRef.current);
      reconnectTimerRef.current = null;
    }
  }, []);

  const closeEventSource = useCallback(() => {
    if (flushTimerRef.current) {
      clearInterval(flushTimerRef.current);
      flushTimerRef.current = null;
    }
    // Flush any remaining buffered entries
    flushBuffer();
    if (eventSourceRef.current) {
      eventSourceRef.current.close();
      eventSourceRef.current = null;
    }
  }, [flushBuffer]);

  const connect = useCallback(() => {
    // Guard: don't connect if unmounted, inactive, or run already terminal
    if (unmountedRef.current || !runId || !isActive) return;
    if (statusRef.current && TERMINAL_STATUSES.has(statusRef.current)) return;

    // Clean up any existing connection first
    closeEventSource();

    const url = `/api/v1/runs/${runId}/logs`;
    const es = new EventSource(url);
    eventSourceRef.current = es;

    // Start periodic flush timer
    flushTimerRef.current = setInterval(flushBuffer, FLUSH_INTERVAL_MS);

    es.onopen = () => {
      // Connection established — reset backoff state
      attemptRef.current = 0;
    };

    es.addEventListener("log", (event) => {
      try {
        const entry: LogEntry = JSON.parse(event.data);
        bufferRef.current.push(entry);
      } catch (e) {
        console.warn("Failed to parse SSE log entry:", e, event.data);
      }
    });

    es.addEventListener("status", (event) => {
      try {
        const data: SSEStatusEvent = JSON.parse(event.data);
        setStatus(data.status);
        statusRef.current = data.status;
        // Run completed — refresh related SWR keys for instant UI update
        void mutate((key: unknown) =>
          KEYS.match.tables(key) || KEYS.match.allLandingFiles(key),
        );
        // Terminal status — close and don't reconnect
        closeEventSource();
        clearReconnectTimer();
      } catch (e) {
        console.warn("Failed to parse SSE status entry:", e, event.data);
      }
    });

    es.onerror = () => {
      closeEventSource();

      // Don't reconnect if unmounted or run reached a terminal status
      if (unmountedRef.current) return;
      if (statusRef.current && TERMINAL_STATUSES.has(statusRef.current)) return;

      const attempt = attemptRef.current;
      if (attempt >= RECONNECT_MAX_ATTEMPTS) {
        console.warn(
          `SSE: giving up after ${RECONNECT_MAX_ATTEMPTS} failed reconnection attempts for run ${runId}`,
        );
        return;
      }

      // Exponential backoff: 1s, 2s, 4s, 8s, 16s, 30s, 30s, ...
      const delay = Math.min(
        RECONNECT_INITIAL_DELAY_MS * Math.pow(2, attempt),
        RECONNECT_MAX_DELAY_MS,
      );
      attemptRef.current = attempt + 1;

      console.debug(
        `SSE: reconnecting in ${delay}ms (attempt ${attempt + 1}/${RECONNECT_MAX_ATTEMPTS}) for run ${runId}`,
      );

      reconnectTimerRef.current = setTimeout(() => {
        reconnectTimerRef.current = null;
        connect();
      }, delay);
    };
  }, [runId, isActive, closeEventSource, clearReconnectTimer, flushBuffer, mutate]);

  useEffect(() => {
    unmountedRef.current = false;

    if (!runId || !isActive) {
      closeEventSource();
      clearReconnectTimer();
      return;
    }

    // Reset attempt counter for a fresh connection
    attemptRef.current = 0;
    connect();

    return () => {
      unmountedRef.current = true;
      closeEventSource();
      clearReconnectTimer();
    };
  }, [runId, isActive, connect, closeEventSource, clearReconnectTimer]);

  return { logs, status };
}
