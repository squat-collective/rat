"use client";

import { useApiClient } from "@/providers/api-provider";
import { useCallback, useState } from "react";
import { useSWRConfig } from "swr";
import { KEYS } from "@/lib/cache-keys";

export function useSaveFile() {
  const api = useApiClient();
  const { mutate } = useSWRConfig();
  const [saving, setSaving] = useState(false);

  const save = useCallback(
    async (path: string, content: string) => {
      setSaving(true);
      try {
        await api.storage.write(path, content);
        // Refresh the file tree and the file's own cache so callers don't
        // have to remember. Resource-specific keys (quality tests, pipelines,
        // etc.) are still the caller's responsibility.
        await mutate(KEYS.match.files);
        await mutate(KEYS.file(path));
      } finally {
        setSaving(false);
      }
    },
    [api, mutate],
  );

  return { save, saving };
}

export type OpenTab = {
  path: string;
  content: string;
  originalContent: string;
  language: string;
};

export function detectLanguage(path: string): string {
  const ext = path.split(".").pop()?.toLowerCase() ?? "";
  switch (ext) {
    case "sql":
      return "sql";
    case "py":
      return "python";
    case "yaml":
    case "yml":
      return "yaml";
    case "json":
      return "json";
    case "md":
      return "markdown";
    case "sh":
      return "shell";
    default:
      return "text";
  }
}
