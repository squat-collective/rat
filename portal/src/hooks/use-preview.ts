"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { useApiClient } from "@/providers/api-provider";
import type { PreviewResponse } from "@squat-collective/rat-client";

const AUTO_PREVIEW_KEY = "rat:autoPreview";

export interface UsePreviewOptions {
  ns: string;
  layer: string;
  name: string;
  getCode?: () => string | undefined;
}

export interface UsePreviewReturn {
  data: PreviewResponse | null;
  loading: boolean;
  error: string | null;
  trigger: (limit?: number) => void;
  autoPreview: boolean;
  setAutoPreview: (enabled: boolean) => void;
  limit: number;
  setLimit: (limit: number) => void;
}

export function usePreview({ ns, layer, name, getCode }: UsePreviewOptions): UsePreviewReturn {
  const api = useApiClient();
  const [data, setData] = useState<PreviewResponse | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [limit, setLimit] = useState(100);
  const [autoPreview, setAutoPreviewState] = useState(() => {
    if (typeof window === "undefined") return false;
    return localStorage.getItem(AUTO_PREVIEW_KEY) === "true";
  });
  const abortRef = useRef<AbortController | null>(null);

  const setAutoPreview = useCallback((enabled: boolean) => {
    setAutoPreviewState(enabled);
    if (typeof window !== "undefined") {
      localStorage.setItem(AUTO_PREVIEW_KEY, String(enabled));
    }
  }, []);

  const trigger = useCallback(
    async (overrideLimit?: number) => {
      // Abort any in-flight request
      if (abortRef.current) {
        abortRef.current.abort();
      }
      abortRef.current = new AbortController();

      setLoading(true);
      setError(null);

      try {
        const code = getCode?.();
        const result = await api.pipelines.preview(ns, layer, name, {
          limit: overrideLimit ?? limit,
          ...(code ? { code } : {}),
        });
        setData(result);
        if (result.error) {
          setError(result.error);
        }
      } catch (e) {
        if (e instanceof DOMException && e.name === "AbortError") return;
        const msg = e instanceof Error ? e.message : String(e);
        setError(msg);
      } finally {
        setLoading(false);
      }
    },
    [api, ns, layer, name, limit, getCode],
  );

  // Cleanup on unmount
  useEffect(() => {
    return () => {
      abortRef.current?.abort();
    };
  }, []);

  return {
    data,
    loading,
    error,
    trigger,
    autoPreview,
    setAutoPreview,
    limit,
    setLimit,
  };
}
