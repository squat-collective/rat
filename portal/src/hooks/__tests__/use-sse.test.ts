/**
 * Tests for the SSE hook's underlying logic patterns.
 *
 * Since useRunLogsSSE is a React hook that requires a DOM environment
 * (EventSource, renderHook) which is not available in the current test setup,
 * we test the algorithmic patterns it relies on: exponential backoff calculation,
 * terminal status detection, log entry parsing/validation, and buffer capping.
 *
 * These mirror the exact logic inside use-sse.ts.
 */
import { describe, it, expect } from "vitest";

// ── Backoff calculation (mirrors use-sse.ts onerror handler) ──
const RECONNECT_INITIAL_DELAY_MS = 1_000;
const RECONNECT_MAX_DELAY_MS = 30_000;
const RECONNECT_MAX_ATTEMPTS = 10;
const MAX_LOG_ENTRIES = 10_000;
const TERMINAL_STATUSES = new Set(["success", "failed", "cancelled"]);

function calculateBackoffDelay(attempt: number): number {
  return Math.min(
    RECONNECT_INITIAL_DELAY_MS * Math.pow(2, attempt),
    RECONNECT_MAX_DELAY_MS,
  );
}

function shouldReconnect(
  attempt: number,
  status: string | null,
  unmounted: boolean,
): boolean {
  if (unmounted) return false;
  if (status && TERMINAL_STATUSES.has(status)) return false;
  if (attempt >= RECONNECT_MAX_ATTEMPTS) return false;
  return true;
}

interface LogEntry {
  timestamp: string;
  level: string;
  message: string;
}

function parseLogEntry(data: string): LogEntry | null {
  try {
    const entry: LogEntry = JSON.parse(data);
    return entry;
  } catch {
    return null;
  }
}

function parseStatusEvent(data: string): string | null {
  try {
    const parsed = JSON.parse(data);
    return parsed.status ?? null;
  } catch {
    return null;
  }
}

function capBuffer(existing: LogEntry[], incoming: LogEntry[]): LogEntry[] {
  const merged = existing.concat(incoming);
  if (merged.length > MAX_LOG_ENTRIES) {
    return merged.slice(merged.length - MAX_LOG_ENTRIES);
  }
  return merged;
}

describe("SSE backoff calculation", () => {
  it("starts at 1s for the first attempt", () => {
    expect(calculateBackoffDelay(0)).toBe(1_000);
  });

  it("doubles each attempt: 1s, 2s, 4s, 8s, 16s", () => {
    expect(calculateBackoffDelay(0)).toBe(1_000);
    expect(calculateBackoffDelay(1)).toBe(2_000);
    expect(calculateBackoffDelay(2)).toBe(4_000);
    expect(calculateBackoffDelay(3)).toBe(8_000);
    expect(calculateBackoffDelay(4)).toBe(16_000);
  });

  it("caps at 30s for attempt 5 and beyond", () => {
    expect(calculateBackoffDelay(5)).toBe(30_000);
    expect(calculateBackoffDelay(6)).toBe(30_000);
    expect(calculateBackoffDelay(7)).toBe(30_000);
    expect(calculateBackoffDelay(9)).toBe(30_000);
  });

  it("handles very large attempt numbers without overflow issues", () => {
    const delay = calculateBackoffDelay(100);
    expect(delay).toBe(30_000);
  });
});

describe("SSE reconnection policy", () => {
  it("allows reconnection when status is null and attempts remain", () => {
    expect(shouldReconnect(0, null, false)).toBe(true);
    expect(shouldReconnect(5, null, false)).toBe(true);
    expect(shouldReconnect(9, null, false)).toBe(true);
  });

  it("blocks reconnection after max attempts (10)", () => {
    expect(shouldReconnect(10, null, false)).toBe(false);
    expect(shouldReconnect(15, null, false)).toBe(false);
  });

  it("blocks reconnection on terminal status: success", () => {
    expect(shouldReconnect(0, "success", false)).toBe(false);
  });

  it("blocks reconnection on terminal status: failed", () => {
    expect(shouldReconnect(0, "failed", false)).toBe(false);
  });

  it("blocks reconnection on terminal status: cancelled", () => {
    expect(shouldReconnect(0, "cancelled", false)).toBe(false);
  });

  it("allows reconnection on non-terminal status: running", () => {
    expect(shouldReconnect(0, "running", false)).toBe(true);
  });

  it("allows reconnection on non-terminal status: pending", () => {
    expect(shouldReconnect(0, "pending", false)).toBe(true);
  });

  it("blocks reconnection when unmounted", () => {
    expect(shouldReconnect(0, null, true)).toBe(false);
  });

  it("blocks reconnection when unmounted even with attempts remaining", () => {
    expect(shouldReconnect(3, "running", true)).toBe(false);
  });
});

describe("SSE log entry parsing", () => {
  it("parses a valid JSON log entry", () => {
    const data = JSON.stringify({
      timestamp: "2026-01-01T00:00:00Z",
      level: "INFO",
      message: "Pipeline started",
    });
    const entry = parseLogEntry(data);
    expect(entry).not.toBeNull();
    expect(entry!.timestamp).toBe("2026-01-01T00:00:00Z");
    expect(entry!.level).toBe("INFO");
    expect(entry!.message).toBe("Pipeline started");
  });

  it("returns null for invalid JSON", () => {
    expect(parseLogEntry("not json")).toBeNull();
    expect(parseLogEntry("")).toBeNull();
    expect(parseLogEntry("{broken")).toBeNull();
  });

  it("returns null for completely empty data", () => {
    expect(parseLogEntry("")).toBeNull();
  });
});

describe("SSE status event parsing", () => {
  it("extracts status from valid event data", () => {
    expect(parseStatusEvent('{"status":"success"}')).toBe("success");
    expect(parseStatusEvent('{"status":"failed"}')).toBe("failed");
    expect(parseStatusEvent('{"status":"running"}')).toBe("running");
  });

  it("returns null for missing status field", () => {
    expect(parseStatusEvent('{"other":"value"}')).toBeNull();
  });

  it("returns null for invalid JSON", () => {
    expect(parseStatusEvent("not json")).toBeNull();
    expect(parseStatusEvent("")).toBeNull();
  });
});

describe("SSE log buffer capping", () => {
  it("returns all entries when under the cap", () => {
    const existing = [{ timestamp: "t1", level: "INFO", message: "m1" }];
    const incoming = [{ timestamp: "t2", level: "INFO", message: "m2" }];
    const result = capBuffer(existing, incoming);
    expect(result).toHaveLength(2);
  });

  it("caps at MAX_LOG_ENTRIES, keeping the newest entries", () => {
    const existing: LogEntry[] = Array.from({ length: 9_999 }, (_, i) => ({
      timestamp: `t${i}`,
      level: "INFO",
      message: `msg-${i}`,
    }));
    const incoming: LogEntry[] = Array.from({ length: 10 }, (_, i) => ({
      timestamp: `new-t${i}`,
      level: "INFO",
      message: `new-msg-${i}`,
    }));

    const result = capBuffer(existing, incoming);
    expect(result).toHaveLength(MAX_LOG_ENTRIES);
    // The newest entries (incoming) should be at the end
    expect(result[result.length - 1].message).toBe("new-msg-9");
    // The oldest entries should have been trimmed from the start
    expect(result[0].message).toBe("msg-9");
  });

  it("returns exactly MAX_LOG_ENTRIES when buffer is at capacity", () => {
    const existing: LogEntry[] = Array.from({ length: MAX_LOG_ENTRIES }, (_, i) => ({
      timestamp: `t${i}`,
      level: "INFO",
      message: `msg-${i}`,
    }));
    const incoming: LogEntry[] = [
      { timestamp: "new-t0", level: "INFO", message: "new-msg" },
    ];

    const result = capBuffer(existing, incoming);
    expect(result).toHaveLength(MAX_LOG_ENTRIES);
    expect(result[result.length - 1].message).toBe("new-msg");
  });

  it("handles empty existing buffer", () => {
    const incoming = [{ timestamp: "t1", level: "INFO", message: "m1" }];
    const result = capBuffer([], incoming);
    expect(result).toHaveLength(1);
  });

  it("handles empty incoming buffer", () => {
    const existing = [{ timestamp: "t1", level: "INFO", message: "m1" }];
    const result = capBuffer(existing, []);
    expect(result).toHaveLength(1);
  });
});

describe("SSE URL construction", () => {
  it("builds correct URL from run ID", () => {
    const runId = "abc-123";
    const url = `/api/v1/runs/${runId}/logs`;
    expect(url).toBe("/api/v1/runs/abc-123/logs");
  });

  it("handles UUID-style run IDs", () => {
    const runId = "550e8400-e29b-41d4-a716-446655440000";
    const url = `/api/v1/runs/${runId}/logs`;
    expect(url).toBe("/api/v1/runs/550e8400-e29b-41d4-a716-446655440000/logs");
  });
});

describe("terminal status set", () => {
  it("includes success, failed, cancelled", () => {
    expect(TERMINAL_STATUSES.has("success")).toBe(true);
    expect(TERMINAL_STATUSES.has("failed")).toBe(true);
    expect(TERMINAL_STATUSES.has("cancelled")).toBe(true);
  });

  it("does not include running or pending", () => {
    expect(TERMINAL_STATUSES.has("running")).toBe(false);
    expect(TERMINAL_STATUSES.has("pending")).toBe(false);
  });

  it("does not include arbitrary strings", () => {
    expect(TERMINAL_STATUSES.has("unknown")).toBe(false);
    expect(TERMINAL_STATUSES.has("")).toBe(false);
  });
});
