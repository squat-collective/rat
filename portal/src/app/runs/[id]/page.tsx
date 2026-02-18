"use client";

import { useParams } from "next/navigation";
import { useRun, useRunLogs } from "@/hooks/use-api";
import { useRunLogsSSE } from "@/hooks/use-sse";
import { useApiClient } from "@/providers/api-provider";
import { Loading } from "@/components/loading";
import { ErrorAlert } from "@/components/error-alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import { STATUS_COLORS, STATUS_EMOJI } from "@/lib/constants";
import Link from "next/link";
import { ArrowLeft, XCircle } from "lucide-react";
import { useState, useMemo, useEffect } from "react";
import { QualityTestDetailDialog } from "@/components/quality-test-detail-dialog";

interface ParsedQualityResult {
  name: string;
  status: "pass" | "fail" | "error";
  rows?: number;
  duration?: number;
  compiledSql?: string;
  sampleRows?: string;
  description?: string;
  errorMessage?: string;
  severity?: string;
}

function parseQualityResults(logs: Array<{ message: string }>): ParsedQualityResult[] {
  const map = new Map<string, Partial<ParsedQualityResult> & { name: string }>();

  const sqlRe = /^Quality test '([^']+)' SQL:\n([\s\S]+)$/;
  const passFailRe = /Quality test '([^']+)':\s*(pass|fail)\s*\((\d+)\s*rows?,\s*(\d+)ms\)/;
  const sampleRe = /^Sample violations for '([^']+)':\n([\s\S]+)$/;
  const descRe = /^Quality test '([^']+)' description:\s*(.+)$/;
  const errorRe = /^Quality test '([^']+)' errored:\s*(.+)$/;
  const severityRe = /^\s*\[(error|warn)]\s*([^:]+):\s*(pass|fail|error)\s*[—–-]\s*(.*)$/;

  const getOrCreate = (name: string) => {
    let entry = map.get(name);
    if (!entry) {
      entry = { name };
      map.set(name, entry);
    }
    return entry;
  };

  for (const log of logs) {
    const msg = log.message;

    let m = sqlRe.exec(msg);
    if (m) { getOrCreate(m[1]).compiledSql = m[2]; continue; }

    m = passFailRe.exec(msg);
    if (m) {
      const e = getOrCreate(m[1]);
      e.status = m[2] as "pass" | "fail";
      e.rows = parseInt(m[3], 10);
      e.duration = parseInt(m[4], 10);
      continue;
    }

    m = sampleRe.exec(msg);
    if (m) { getOrCreate(m[1]).sampleRows = m[2]; continue; }

    m = descRe.exec(msg);
    if (m) { getOrCreate(m[1]).description = m[2]; continue; }

    m = errorRe.exec(msg);
    if (m) {
      const e = getOrCreate(m[1]);
      e.status = "error";
      e.errorMessage = m[2];
      continue;
    }

    m = severityRe.exec(msg);
    if (m) { getOrCreate(m[2]).severity = m[1]; continue; }
  }

  return Array.from(map.values())
    .filter((e): e is ParsedQualityResult => !!e.status)
    .map((e) => ({ ...e, status: e.status! }));
}

const LOG_LEVEL_COLORS: Record<string, string> = {
  INFO: "text-primary",
  WARN: "text-yellow-400",
  ERROR: "text-destructive",
  DEBUG: "text-muted-foreground",
};

export default function RunDetailPage() {
  const params = useParams<{ id: string }>();
  const api = useApiClient();
  const { data: run, isLoading, error, mutate: mutateRun } = useRun(params.id);
  const { data: logsData } = useRunLogs(params.id);
  const [cancelling, setCancelling] = useState(false);
  const [selectedTest, setSelectedTest] = useState<ParsedQualityResult | null>(null);

  const isActive = !!run && ["pending", "running"].includes(run.status);

  // SSE for live logs on active runs
  const { logs: sseLogs, status: sseStatus } = useRunLogsSSE(params.id, isActive);

  // When SSE reports completion, refresh the run data
  useEffect(() => {
    if (sseStatus && run && ["pending", "running"].includes(run.status)) {
      void mutateRun();
    }
  }, [sseStatus, run, mutateRun]);

  // Use SSE logs for active runs, polled logs for terminal runs
  const displayLogs = useMemo(() => {
    if (isActive && sseLogs.length > 0) return sseLogs;
    return logsData?.logs ?? [];
  }, [isActive, sseLogs, logsData?.logs]);

  const qualityResults = useMemo(
    () => parseQualityResults(displayLogs),
    [displayLogs],
  );

  if (isLoading) return <Loading text="Loading run..." />;
  if (error) return <ErrorAlert error={error} prefix="Failed to load run" />;
  if (!run) {
    return <div className="error-block p-4 text-xs">Run not found</div>;
  }

  const canCancel = ["pending", "running"].includes(run.status);

  const handleCancel = async () => {
    setCancelling(true);
    try {
      await api.runs.cancel(params.id);
    } finally {
      setCancelling(false);
    }
  };

  return (
    <div className="space-y-6">
      {/* Back + Header */}
      <div>
        <Link
          href="/runs"
          className="text-[10px] text-muted-foreground hover:text-primary flex items-center gap-1 mb-2"
        >
          <ArrowLeft className="h-3 w-3" /> Back to runs
        </Link>
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-3">
            <h1 className="text-sm font-bold tracking-wider font-mono">
              {STATUS_EMOJI[run.status] || ""} {run.id.slice(0, 12)}
            </h1>
            <Badge
              variant="outline"
              className={cn("text-[9px]", STATUS_COLORS[run.status] || "")}
            >
              {run.status}
            </Badge>
          </div>
          {canCancel && (
            <Button
              size="sm"
              variant="destructive"
              onClick={handleCancel}
              disabled={cancelling}
              className="gap-1"
            >
              <XCircle className="h-3 w-3" />
              {cancelling ? "Cancelling..." : "Cancel Run"}
            </Button>
          )}
        </div>
      </div>

      {/* Info cards */}
      <div className="grid grid-cols-2 lg:grid-cols-4 gap-3">
        <div className="brutal-card bg-card p-3">
          <p className="text-[10px] tracking-wider text-muted-foreground">
            Duration
          </p>
          <p className="text-xs font-medium mt-1">
            {run.duration_ms ? `${run.duration_ms}ms` : "-"}
          </p>
        </div>
        <div className="brutal-card bg-card p-3">
          <p className="text-[10px] tracking-wider text-muted-foreground">
            Rows Written
          </p>
          <p className="text-xs font-medium mt-1">
            {run.rows_written ?? "-"}
          </p>
        </div>
        <div className="brutal-card bg-card p-3">
          <p className="text-[10px] tracking-wider text-muted-foreground">
            Trigger
          </p>
          <p className="text-xs font-medium mt-1">{run.trigger}</p>
        </div>
        <div className="brutal-card bg-card p-3">
          <p className="text-[10px] tracking-wider text-muted-foreground">
            Started
          </p>
          <p className="text-xs font-medium mt-1">
            {run.started_at
              ? new Date(run.started_at).toLocaleString()
              : "-"}
          </p>
        </div>
      </div>

      {/* Quality Test Results */}
      {qualityResults.length > 0 && (
        <div className="brutal-card bg-card p-4 space-y-2">
          <div className="flex items-center gap-3">
            <span className="text-xs font-bold tracking-wider text-muted-foreground">
              {"\uD83E\uDDEA"} Quality Tests
            </span>
            <span className="text-[10px] text-green-400">
              {"\u2705"} {qualityResults.filter((r) => r.status === "pass").length} passed
            </span>
            {qualityResults.filter((r) => r.status !== "pass" && r.severity === "warn").length > 0 && (
              <span className="text-[10px] text-yellow-400">
                {"\u26A0\uFE0F"} {qualityResults.filter((r) => r.status !== "pass" && r.severity === "warn").length} warned
              </span>
            )}
            {qualityResults.filter((r) => r.status !== "pass" && r.severity !== "warn").length > 0 && (
              <span className="text-[10px] text-destructive">
                {"\u274C"} {qualityResults.filter((r) => r.status !== "pass" && r.severity !== "warn").length} failed
              </span>
            )}
          </div>
          <div className="space-y-1">
            {qualityResults.map((r) => (
              <div
                key={r.name}
                className="flex items-center gap-2 text-[10px] font-mono cursor-pointer hover:bg-primary/5 px-1 py-0.5 -mx-1 transition-colors"
                onClick={() => {
                  // For live SSE runs, look up latest data by name
                  const latest = qualityResults.find((q) => q.name === r.name) ?? r;
                  setSelectedTest(latest);
                }}
              >
                <span>
                  {r.status === "pass"
                    ? "\u2705"
                    : r.severity === "warn"
                      ? "\u26A0\uFE0F"
                      : "\u274C"}
                </span>
                <span className="font-medium">{r.name}</span>
                {r.severity && (
                  <Badge
                    variant="outline"
                    className={cn(
                      "text-[8px] px-1 py-0",
                      r.severity === "warn"
                        ? "text-yellow-400 border-yellow-700"
                        : "text-destructive border-destructive/50",
                    )}
                  >
                    {r.severity}
                  </Badge>
                )}
                {r.status === "error" ? (
                  <span className="text-destructive">error</span>
                ) : (
                  <>
                    <span className={
                      r.status === "pass"
                        ? "text-green-400"
                        : r.severity === "warn"
                          ? "text-yellow-400"
                          : "text-destructive"
                    }>
                      {r.status === "fail" && r.severity === "warn" ? "warned" : r.status}
                    </span>
                    {r.duration !== undefined && (
                      <span className="text-muted-foreground">{r.duration}ms</span>
                    )}
                    {r.status === "fail" && r.rows !== undefined && r.rows > 0 && (
                      <span className="text-muted-foreground">{r.rows} rows</span>
                    )}
                  </>
                )}
              </div>
            ))}
          </div>
        </div>
      )}

      {/* Error */}
      {run.error && (
        <div className="error-block px-4 py-3 text-xs text-destructive">
          {run.error}
        </div>
      )}

      {/* Logs */}
      <div>
        <h2 className="text-xs font-bold tracking-wider text-muted-foreground mb-2">
          Logs
        </h2>
        <div className="border-2 border-border/50 bg-card/50 overflow-auto max-h-[400px] p-3 font-mono text-[11px]">
          {displayLogs.length > 0 ? (
            displayLogs.map((log, i) => (
              <div key={i} className="flex gap-2">
                <span className="text-muted-foreground/50 shrink-0">
                  {log.timestamp}
                </span>
                <span
                  className={cn(
                    "shrink-0 w-12",
                    LOG_LEVEL_COLORS[log.level] || "",
                  )}
                >
                  [{log.level}]
                </span>
                <span>{log.message}</span>
              </div>
            ))
          ) : (
            <span className="text-muted-foreground">
              {run.status === "pending"
                ? "Waiting for logs..."
                : "No logs available"}
            </span>
          )}
        </div>
      </div>

      {/* Full ID */}
      <div className="text-[10px] text-muted-foreground font-mono">
        run_id: {run.id}
      </div>

      {/* Quality Test Detail Dialog */}
      <QualityTestDetailDialog
        result={selectedTest}
        open={!!selectedTest}
        onOpenChange={(open) => {
          if (!open) setSelectedTest(null);
        }}
      />
    </div>
  );
}
