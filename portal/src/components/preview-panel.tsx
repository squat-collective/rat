"use client";

import { useState } from "react";
import type {
  PreviewResponse,
  PhaseProfile as PhaseProfileType,
  PreviewLogEntry,
  PreviewColumn,
} from "@squat-collective/rat-client";
import { DataTable } from "@/components/data-table";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { cn, formatBytes } from "@/lib/utils";
import {
  Play,
  Loader2,
  ToggleLeft,
  ToggleRight,
  GripHorizontal,
  AlertTriangle,
} from "lucide-react";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/components/ui/tooltip";

interface PreviewPanelProps {
  data: PreviewResponse | null;
  loading: boolean;
  error: string | null;
  onTrigger: (limit?: number) => void;
  autoPreview: boolean;
  onAutoPreviewChange: (enabled: boolean) => void;
  limit: number;
  onLimitChange: (limit: number) => void;
  isQualityTest?: boolean;
  qualityTestName?: string;
}

const LEVEL_COLORS: Record<string, string> = {
  info: "text-blue-400",
  warn: "text-yellow-400",
  error: "text-red-400",
  debug: "text-gray-400",
};

export function PreviewPanel({
  data,
  loading,
  error,
  onTrigger,
  autoPreview,
  onAutoPreviewChange,
  limit,
  onLimitChange,
  isQualityTest,
  qualityTestName,
}: PreviewPanelProps) {
  const [activeTab, setActiveTab] = useState("results");

  const totalDuration = data?.phases?.reduce(
    (sum: number, p: PhaseProfileType) => sum + p.duration_ms,
    0,
  );

  return (
    <div className="flex flex-col h-full border-t-2 border-primary/30 bg-card/50">
      {/* Drag handle (visual only — resizing handled by parent) */}
      <div className="flex items-center justify-center h-2 cursor-row-resize bg-border/20 hover:bg-primary/20 transition-colors">
        <GripHorizontal className="h-3 w-3 text-muted-foreground/50" />
      </div>

      {/* Toolbar */}
      <div className="flex items-center gap-2 px-3 py-1.5 border-b border-border/50">
        <Button
          size="sm"
          variant="default"
          onClick={() => onTrigger()}
          disabled={loading}
          className="h-6 text-[10px] gap-1"
        >
          {loading ? (
            <Loader2 className="h-3 w-3 animate-spin" />
          ) : (
            <Play className="h-3 w-3" />
          )}
          {loading ? "Running..." : "Preview"}
        </Button>

        <span className="text-[9px] text-muted-foreground font-mono">
          {"\u21E7\u2318\u23CE"}
        </span>

        <TooltipProvider delayDuration={200}>
          <Tooltip>
            <TooltipTrigger asChild>
              <span className="flex items-center gap-0.5 text-[9px] text-yellow-500">
                <AlertTriangle className="h-3 w-3" />
              </span>
            </TooltipTrigger>
            <TooltipContent side="bottom" className="text-[10px] max-w-60">
              Preview executes real SQL against your data. No writes are performed, but complex queries may be slow.
            </TooltipContent>
          </Tooltip>
        </TooltipProvider>

        <div className="h-4 w-px bg-border/50" />

        {/* Auto-preview toggle */}
        <button
          onClick={() => onAutoPreviewChange(!autoPreview)}
          className="flex items-center gap-1 text-[10px] text-muted-foreground hover:text-foreground transition-colors"
        >
          {autoPreview ? (
            <ToggleRight className="h-3.5 w-3.5 text-primary" />
          ) : (
            <ToggleLeft className="h-3.5 w-3.5" />
          )}
          Auto
        </button>

        <div className="h-4 w-px bg-border/50" />

        {/* Row limit selector */}
        <Select
          value={String(limit)}
          onValueChange={(v) => onLimitChange(Number(v))}
        >
          <SelectTrigger className="h-6 w-16 text-[10px]">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="50">50</SelectItem>
            <SelectItem value="100">100</SelectItem>
            <SelectItem value="500">500</SelectItem>
          </SelectContent>
        </Select>
        <span className="text-[10px] text-muted-foreground">rows</span>

        {/* Status info */}
        <div className="flex-1" />
        {isQualityTest && qualityTestName && (
          <span className="text-[10px] font-mono text-muted-foreground mr-2">
            🧪 {qualityTestName}
          </span>
        )}
        {data && !error && isQualityTest && (
          <>
            {(data.rows?.length ?? 0) === 0 ? (
              <Badge variant="outline" className="text-[9px] border-green-500/50 text-green-400">
                PASS — no violations
              </Badge>
            ) : (
              <Badge variant="outline" className="text-[9px] border-destructive/50 text-destructive">
                FAIL — {data.rows?.length} violation(s)
              </Badge>
            )}
            {totalDuration != null && (
              <span className="text-[10px] text-muted-foreground font-mono ml-2">
                {totalDuration}ms
              </span>
            )}
          </>
        )}
        {data && !error && !isQualityTest && (
          <span className="text-[10px] text-muted-foreground font-mono">
            {data.rows?.length ?? 0} of {data.total_row_count} rows
            {totalDuration != null && ` \u2022 ${totalDuration}ms`}
          </span>
        )}
        {error && (
          <Badge variant="destructive" className="text-[9px]">
            Error
          </Badge>
        )}
      </div>

      {/* Content tabs */}
      <Tabs
        value={activeTab}
        onValueChange={setActiveTab}
        className="flex-1 flex flex-col min-h-0"
      >
        <TabsList className="h-7 px-3 justify-start bg-transparent border-b border-border/50 rounded-none">
          <TabsTrigger value="results" className="text-[10px] h-6">
            {isQualityTest ? "Violations" : "Results"}
          </TabsTrigger>
          <TabsTrigger value="logs" className="text-[10px] h-6">
            Logs
            {data?.logs && data.logs.length > 0 && (
              <Badge
                variant="secondary"
                className="ml-1 text-[8px] h-3 px-1"
              >
                {data.logs.length}
              </Badge>
            )}
          </TabsTrigger>
          <TabsTrigger value="profile" className="text-[10px] h-6">
            Profile
          </TabsTrigger>
          <TabsTrigger value="explain" className="text-[10px] h-6">
            Explain
          </TabsTrigger>
        </TabsList>

        {/* Results tab */}
        <TabsContent value="results" className="flex-1 overflow-hidden m-0 p-0">
          {error && !data?.rows?.length ? (
            <div className="p-4 text-xs text-destructive font-mono whitespace-pre-wrap">
              {error}
            </div>
          ) : data?.rows && data.rows.length > 0 ? (
            <DataTable
              columns={data.columns?.map((c: PreviewColumn) => c.name) ?? []}
              rows={data.rows}
              maxHeight="100%"
            />
          ) : data && isQualityTest ? (
            <div className="flex items-center justify-center h-full gap-2">
              <Badge variant="outline" className="text-[10px] border-green-500/50 text-green-400">
                ✅ PASS — no violations found
              </Badge>
            </div>
          ) : (
            <div className="flex items-center justify-center h-full text-[10px] text-muted-foreground">
              {loading
                ? "Executing preview..."
                : "Click Preview or press \u21E7\u2318\u23CE to run"}
            </div>
          )}
        </TabsContent>

        {/* Logs tab */}
        <TabsContent
          value="logs"
          className="flex-1 overflow-auto m-0 p-0 font-mono"
        >
          {data?.logs && data.logs.length > 0 ? (
            <div className="p-2 space-y-0.5">
              {data.logs.map((log: PreviewLogEntry, i: number) => (
                <div key={i} className="flex gap-2 text-[10px]">
                  <span className="text-muted-foreground/50 w-20 shrink-0 truncate">
                    {log.timestamp
                      ? new Date(
                          Number(log.timestamp) * 1000,
                        ).toLocaleTimeString()
                      : ""}
                  </span>
                  <span
                    className={cn(
                      "w-10 shrink-0 font-bold",
                      LEVEL_COLORS[log.level] ?? "text-foreground",
                    )}
                  >
                    {log.level}
                  </span>
                  <span className="text-foreground/80">{log.message}</span>
                </div>
              ))}
            </div>
          ) : (
            <div className="flex items-center justify-center h-full text-[10px] text-muted-foreground">
              No logs yet
            </div>
          )}
        </TabsContent>

        {/* Profile tab */}
        <TabsContent value="profile" className="flex-1 overflow-auto m-0 p-3">
          {data?.phases && data.phases.length > 0 ? (
            <div className="space-y-3">
              {/* Phase timeline bars */}
              <div className="space-y-1.5">
                {data.phases.map((phase: PhaseProfileType) => {
                  const maxDuration = Math.max(
                    ...data.phases.map((p: PhaseProfileType) => p.duration_ms),
                    1,
                  );
                  const pct = (phase.duration_ms / maxDuration) * 100;
                  return (
                    <div key={phase.name} className="flex items-center gap-2">
                      <span className="text-[10px] text-muted-foreground w-16 shrink-0 font-mono">
                        {phase.name}
                      </span>
                      <div className="flex-1 h-4 bg-muted/30 relative">
                        <div
                          className="h-full bg-primary/60"
                          style={{ width: `${Math.max(pct, 2)}%` }}
                        />
                      </div>
                      <span className="text-[10px] font-mono text-muted-foreground w-14 text-right shrink-0">
                        {phase.duration_ms}ms
                      </span>
                    </div>
                  );
                })}
              </div>

              {/* Summary stats */}
              <div className="grid grid-cols-3 gap-3 brutal-card p-3">
                <div>
                  <p className="text-[9px] tracking-wider text-muted-foreground">
                    Total Time
                  </p>
                  <p className="text-xs font-mono font-bold">
                    {totalDuration}ms
                  </p>
                </div>
                <div>
                  <p className="text-[9px] tracking-wider text-muted-foreground">
                    Memory Peak
                  </p>
                  <p className="text-xs font-mono font-bold">
                    {formatBytes(data.memory_peak_bytes)}
                  </p>
                </div>
                <div>
                  <p className="text-[9px] tracking-wider text-muted-foreground">
                    Total Rows
                  </p>
                  <p className="text-xs font-mono font-bold">
                    {data.total_row_count.toLocaleString()}
                  </p>
                </div>
              </div>

              {/* Warnings */}
              {data.warnings && data.warnings.length > 0 && (
                <div className="space-y-1">
                  {data.warnings.map((w: string, i: number) => (
                    <div
                      key={i}
                      className="text-[10px] text-yellow-400 font-mono"
                    >
                      {"\u26A0"} {w}
                    </div>
                  ))}
                </div>
              )}
            </div>
          ) : (
            <div className="flex items-center justify-center h-full text-[10px] text-muted-foreground">
              No profiling data yet
            </div>
          )}
        </TabsContent>

        {/* Explain tab */}
        <TabsContent
          value="explain"
          className="flex-1 overflow-auto m-0 p-0"
        >
          {data?.explain_output ? (
            <pre className="p-3 text-[10px] font-mono text-foreground/80 whitespace-pre-wrap">
              {data.explain_output}
            </pre>
          ) : (
            <div className="flex items-center justify-center h-full text-[10px] text-muted-foreground">
              {data?.phases?.some(
                (p: PhaseProfileType) =>
                  p.name === "explain" && p.metadata?.skipped,
              )
                ? "EXPLAIN not available for Python pipelines"
                : "No explain output yet"}
            </div>
          )}
        </TabsContent>
      </Tabs>
    </div>
  );
}
