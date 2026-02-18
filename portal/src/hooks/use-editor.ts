"use client";

import { useApiClient } from "@/providers/api-provider";
import { useCallback, useState } from "react";

export function useSaveFile() {
  const api = useApiClient();
  const [saving, setSaving] = useState(false);

  const save = useCallback(
    async (path: string, content: string) => {
      setSaving(true);
      try {
        await api.storage.write(path, content);
      } finally {
        setSaving(false);
      }
    },
    [api],
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
