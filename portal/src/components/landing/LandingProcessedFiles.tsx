"use client";

import { useCallback, useMemo, useState } from "react";
import Link from "next/link";
import { useRun } from "@/hooks/use-api";
import { useApiClient } from "@/providers/api-provider";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { FilePreviewModal } from "@/components/file-preview-modal";
import { cn, formatBytes } from "@/lib/utils";
import { STATUS_COLORS, STATUS_EMOJI } from "@/lib/constants";
import { ChevronDown, ChevronRight, Download, Package } from "lucide-react";
import { formatDate } from "./utils";
import type { FileInfo, FileListResponse } from "@squat-collective/rat-client";

/**
 * Sanitize a filename extracted from an S3 path to prevent path traversal.
 * Strips directory separators and parent-directory sequences, and falls back
 * to "download" if the result is empty.
 */
function sanitizeFilename(raw: string): string {
  // Take only the last segment (basename), strip path traversal sequences
  const basename = raw.split("/").pop()?.split("\\").pop() ?? "";
  const cleaned = basename.replace(/\.\./g, "").replace(/[/\\]/g, "").trim();
  return cleaned || "download";
}

// UUID v4 pattern -- only fetch run metadata for valid platform IDs
const UUID_RE = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

interface ProcessedRunGroup {
  runId: string;
  files: FileInfo[];
  latestModified: string;
}

function ProcessedRunHeader({ runId }: { runId: string }) {
  const isUuid = UUID_RE.test(runId);
  const { data: run } = useRun(isUuid ? runId : "");

  const label = (
    <span className="font-mono text-[10px] text-primary">
      {runId.slice(0, 12)}
    </span>
  );

  if (!isUuid) return label;

  return (
    <span className="flex items-center gap-2">
      <Link
        href={`/runs/${runId}`}
        className="font-mono text-[10px] text-primary hover:underline"
      >
        {runId.slice(0, 12)}
      </Link>
      {run && (
        <>
          <Badge
            variant="outline"
            className={cn("text-[9px]", STATUS_COLORS[run.status] ?? "")}
          >
            {STATUS_EMOJI[run.status] ?? ""} {run.status}
          </Badge>
          {run.started_at && (
            <span className="text-[9px] text-muted-foreground">
              {formatDate(run.started_at)}
            </span>
          )}
        </>
      )}
    </span>
  );
}

interface LandingProcessedFilesProps {
  ns: string;
  processedData: FileListResponse | undefined;
  onError: () => void;
}

export function LandingProcessedFiles({ ns, processedData, onError }: LandingProcessedFilesProps) {
  const api = useApiClient();

  const [processedOpen, setProcessedOpen] = useState(false);
  const [expandedRuns, setExpandedRuns] = useState<Set<string>>(new Set());
  const [downloading, setDownloading] = useState<string | null>(null);

  const processedGroups = useMemo(() => {
    const files = processedData?.files ?? [];
    const groups = new Map<string, FileInfo[]>();

    // Internal S3 subfolder names that are NOT run IDs
    const IGNORED_FOLDERS = new Set(["_processed", "_samples", "_meta"]);

    for (const f of files) {
      // Path: {ns}/landing/{name}/_processed/{run_id}/filename
      // or legacy: {ns}/landing/{name}/_processed/filename
      const processedIdx = f.path.indexOf("/_processed/");
      if (processedIdx === -1) continue;
      const relativePath = f.path.slice(processedIdx + "/_processed/".length);
      const slashIdx = relativePath.indexOf("/");
      if (slashIdx === -1) {
        // Legacy flat file (no run_id subfolder)
        const existing = groups.get("__legacy__") ?? [];
        existing.push(f);
        groups.set("__legacy__", existing);
      } else {
        const runId = relativePath.slice(0, slashIdx);
        // Skip internal subfolder names that aren't run IDs
        if (IGNORED_FOLDERS.has(runId)) continue;
        const existing = groups.get(runId) ?? [];
        existing.push(f);
        groups.set(runId, existing);
      }
    }

    // Build sorted array: newest first by latest file modified date
    const result: ProcessedRunGroup[] = [];
    for (const [runId, groupFiles] of groups) {
      if (runId === "__legacy__") continue;
      const latestModified = groupFiles.reduce(
        (max, f) => (f.modified > max ? f.modified : max),
        "",
      );
      result.push({ runId, files: groupFiles, latestModified });
    }
    result.sort((a, b) => b.latestModified.localeCompare(a.latestModified));

    // Append legacy group at the bottom
    const legacy = groups.get("__legacy__");
    if (legacy) {
      const latestModified = legacy.reduce(
        (max, f) => (f.modified > max ? f.modified : max),
        "",
      );
      result.push({ runId: "__legacy__", files: legacy, latestModified });
    }

    return result;
  }, [processedData]);

  const handleDownloadProcessed = useCallback(
    async (path: string, filename: string) => {
      setDownloading(path);
      try {
        const file = await api.storage.read(path);
        const blob = new Blob([file.content], { type: "application/octet-stream" });
        const url = URL.createObjectURL(blob);
        const a = document.createElement("a");
        a.href = url;
        a.download = sanitizeFilename(filename);
        a.click();
        URL.revokeObjectURL(url);
      } catch (e) {
        console.error("Failed to download processed file:", e);
        onError();
      } finally {
        setDownloading(null);
      }
    },
    [api, onError],
  );

  return (
    <div className="border-2 border-border/50">
      <button
        type="button"
        className="w-full flex items-center gap-2 px-3 py-2.5 text-left hover:bg-muted/30 transition-colors"
        onClick={() => setProcessedOpen(!processedOpen)}
      >
        {processedOpen ? (
          <ChevronDown className="h-3 w-3 text-muted-foreground" />
        ) : (
          <ChevronRight className="h-3 w-3 text-muted-foreground" />
        )}
        <Package className="h-3 w-3 text-muted-foreground" />
        <span className="text-[10px] font-bold tracking-wider">
          Processed Files
        </span>
        <span className="text-[9px] text-muted-foreground">
          &middot; Files archived by pipeline runs
        </span>
        {processedGroups.length > 0 && (
          <Badge variant="outline" className="text-[9px] ml-auto">
            {processedGroups.length} run{processedGroups.length !== 1 ? "s" : ""}
          </Badge>
        )}
      </button>
      {processedOpen && (
        <div className="border-t border-border/30 p-3 space-y-2">
          {processedGroups.length === 0 && (
            <p className="text-center text-[10px] text-muted-foreground py-2">
              No processed files yet. Files will appear here after pipeline runs with archiving enabled.
            </p>
          )}
          {processedGroups.map((group) => {
            const isExpanded = expandedRuns.has(group.runId);
            const toggleExpand = () => {
              setExpandedRuns((prev) => {
                const next = new Set(prev);
                if (next.has(group.runId)) next.delete(group.runId);
                else next.add(group.runId);
                return next;
              });
            };
            return (
              <div key={group.runId} className="border border-border/30">
                <button
                  type="button"
                  className="w-full flex items-center gap-2 px-3 py-2 text-left hover:bg-muted/30 transition-colors"
                  onClick={toggleExpand}
                >
                  {isExpanded ? (
                    <ChevronDown className="h-3 w-3 text-muted-foreground" />
                  ) : (
                    <ChevronRight className="h-3 w-3 text-muted-foreground" />
                  )}
                  {group.runId === "__legacy__" ? (
                    <span className="text-[10px] font-mono text-muted-foreground">
                      Legacy (pre-tracking)
                    </span>
                  ) : (
                    <ProcessedRunHeader runId={group.runId} />
                  )}
                  <span className="text-[9px] text-muted-foreground ml-auto">
                    {group.files.length} file{group.files.length !== 1 ? "s" : ""}
                  </span>
                </button>
                {isExpanded && (
                  <div className="border-t border-border/20">
                    <table className="w-full text-xs">
                      <thead>
                        <tr>
                          <th className="whitespace-nowrap px-3 py-1.5 text-left text-[10px] font-bold tracking-wider text-muted-foreground border-b border-border/30">
                            Filename
                          </th>
                          <th className="whitespace-nowrap px-3 py-1.5 text-left text-[10px] font-bold tracking-wider text-muted-foreground border-b border-border/30">
                            Size
                          </th>
                          <th className="whitespace-nowrap px-3 py-1.5 text-left text-[10px] font-bold tracking-wider text-muted-foreground border-b border-border/30">
                            Date
                          </th>
                          <th className="whitespace-nowrap px-3 py-1.5 text-right text-[10px] font-bold tracking-wider text-muted-foreground border-b border-border/30">
                            Actions
                          </th>
                        </tr>
                      </thead>
                      <tbody>
                        {group.files.map((f) => {
                          const filename = f.path.split("/").pop() ?? f.path;
                          return (
                            <tr
                              key={f.path}
                              className="group border-t border-border/20 hover:bg-primary/5"
                            >
                              <td className="whitespace-nowrap px-3 py-1.5 font-mono font-medium">
                                {filename}
                              </td>
                              <td className="whitespace-nowrap px-3 py-1.5 font-mono text-muted-foreground">
                                {formatBytes(f.size)}
                              </td>
                              <td className="whitespace-nowrap px-3 py-1.5 text-muted-foreground">
                                {formatDate(f.modified)}
                              </td>
                              <td className="whitespace-nowrap px-3 py-1.5 text-right">
                                <div className="flex items-center justify-end gap-1">
                                  <FilePreviewModal
                                    s3Path={f.path}
                                    filename={filename}
                                    namespace={ns}
                                  />
                                  <Button
                                    variant="ghost"
                                    size="sm"
                                    className="h-6 px-2 text-[10px]"
                                    disabled={downloading === f.path}
                                    onClick={() => handleDownloadProcessed(f.path, filename)}
                                  >
                                    <Download className="h-3 w-3" />
                                  </Button>
                                </div>
                              </td>
                            </tr>
                          );
                        })}
                      </tbody>
                    </table>
                  </div>
                )}
              </div>
            );
          })}
        </div>
      )}
    </div>
  );
}
