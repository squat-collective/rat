"use client";

import { useState, useCallback, useMemo } from "react";
import type { MergeStrategy } from "@squat-collective/rat-client";
import { Badge } from "@/components/ui/badge";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Label } from "@/components/ui/label";
import { Copy, Check, AlertTriangle, Loader2 } from "lucide-react";
import {
  extractAnnotations,
  generateStrategySnippet,
  type ParsedStrategyConfig,
} from "@/lib/annotations";

interface PipelineMergeStrategyProps {
  ns: string;
  layer: string;
  name: string;
  sourceCode: string | null;
  pipelineType: string;
}

const STRATEGIES: { value: MergeStrategy; label: string; description: string }[] = [
  { value: "full_refresh", label: "Full Refresh", description: "Overwrite entire table each run" },
  { value: "incremental", label: "Incremental", description: "Merge new rows using unique key (dedup)" },
  { value: "append_only", label: "Append Only", description: "Always append, never overwrite" },
  { value: "delete_insert", label: "Delete + Insert", description: "Delete matching keys, insert all new (no dedup)" },
  { value: "scd2", label: "SCD Type 2", description: "Track history with valid_from/valid_to" },
  { value: "snapshot", label: "Snapshot", description: "Replace only touched partitions" },
];

const STRATEGY_COLORS: Record<MergeStrategy, string> = {
  full_refresh: "bg-zinc-500/20 text-zinc-400 border-zinc-500/30",
  incremental: "bg-green-500/20 text-green-400 border-green-500/30",
  append_only: "bg-blue-500/20 text-blue-400 border-blue-500/30",
  delete_insert: "bg-orange-500/20 text-orange-400 border-orange-500/30",
  scd2: "bg-purple-500/20 text-purple-400 border-purple-500/30",
  snapshot: "bg-cyan-500/20 text-cyan-400 border-cyan-500/30",
};

/** Fields that are required (not optional) per strategy. */
const REQUIRED_FIELDS: Record<MergeStrategy, { field: string; label: string }[]> = {
  full_refresh: [],
  incremental: [{ field: "unique_key", label: "@unique_key" }],
  append_only: [],
  delete_insert: [{ field: "unique_key", label: "@unique_key" }],
  scd2: [
    { field: "unique_key", label: "@unique_key" },
    { field: "scd_valid_from", label: "@scd_valid_from" },
    { field: "scd_valid_to", label: "@scd_valid_to" },
  ],
  snapshot: [{ field: "partition_column", label: "@partition_column" }],
};

/** Detected annotation fields to display. */
const DISPLAY_FIELDS: { key: keyof ParsedStrategyConfig; label: string; defaultDisplay?: string }[] = [
  { key: "merge_strategy", label: "merge_strategy" },
  { key: "unique_key", label: "unique_key" },
  { key: "watermark_column", label: "watermark_column" },
  { key: "partition_column", label: "partition_column" },
  { key: "scd_valid_from", label: "scd_valid_from" },
  { key: "scd_valid_to", label: "scd_valid_to" },
  { key: "materialized", label: "materialized", defaultDisplay: "table" },
  { key: "archive_landing_zones", label: "archive_landing_zones", defaultDisplay: "disabled" },
];

export function PipelineMergeStrategy({
  ns,
  layer,
  name,
  sourceCode,
  pipelineType,
}: PipelineMergeStrategyProps) {
  const commentPrefix = pipelineType === "python" ? "#" : "--";

  // Parse annotations from source code
  const parsed = useMemo<ParsedStrategyConfig | null>(
    () => (sourceCode ? extractAnnotations(sourceCode) : null),
    [sourceCode],
  );

  const strategy = parsed?.merge_strategy ?? null;
  const isKnownStrategy = strategy !== null && STRATEGIES.some((s) => s.value === strategy);
  const strategyInfo = STRATEGIES.find((s) => s.value === strategy);
  const strategyColor = strategy && isKnownStrategy ? STRATEGY_COLORS[strategy] : "bg-yellow-500/20 text-yellow-400 border-yellow-500/30";

  // Missing required fields warning
  const missingFields = useMemo(() => {
    if (!strategy || !isKnownStrategy || !parsed) return [];
    const required = REQUIRED_FIELDS[strategy] ?? [];
    return required.filter(({ field }) => {
      const val = parsed[field as keyof ParsedStrategyConfig];
      return val === null || val === undefined;
    });
  }, [strategy, isKnownStrategy, parsed]);

  // Snippet preview for other strategies
  const [snippetStrategy, setSnippetStrategy] = useState<MergeStrategy | "">("");
  const snippet = useMemo(
    () => snippetStrategy ? generateStrategySnippet(snippetStrategy, commentPrefix as "--" | "#") : null,
    [snippetStrategy, commentPrefix],
  );

  // Copy to clipboard
  const [copied, setCopied] = useState(false);
  const handleCopy = useCallback(async (text: string) => {
    await navigator.clipboard.writeText(text);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  }, []);

  // --- Build current annotations snippet for display ---
  const currentSnippet = useMemo(() => {
    if (!parsed) return null;
    const lines: string[] = [];
    for (const { key, label } of DISPLAY_FIELDS) {
      const val = parsed[key];
      if (val !== null && val !== undefined) {
        const display = Array.isArray(val) ? val.join(", ") : String(val);
        lines.push(`${commentPrefix} @${label}: ${display}`);
      }
    }
    return lines.length > 0 ? lines.join("\n") : null;
  }, [parsed, commentPrefix]);

  // Loading state
  if (sourceCode === null) {
    return (
      <div className="brutal-card bg-card p-4">
        <div className="flex items-center gap-2 text-xs text-muted-foreground">
          <Loader2 className="h-3 w-3 animate-spin" />
          Loading pipeline source...
        </div>
      </div>
    );
  }

  return (
    <div className="space-y-4">
      {/* ── Strategy display (read-only from code) ── */}
      <div className="brutal-card bg-card p-4 space-y-4">
        <div className="flex items-center justify-between">
          <h2 className="text-xs font-bold tracking-wider text-muted-foreground">
            Merge Strategy
          </h2>
          {strategy ? (
            <Badge variant="outline" className={`text-[10px] ${strategyColor}`}>
              {strategyInfo?.label ?? strategy}
            </Badge>
          ) : (
            <Badge variant="outline" className="text-[10px] bg-zinc-500/20 text-zinc-400 border-zinc-500/30">
              full_refresh (default)
            </Badge>
          )}
        </div>

        {/* No annotations detected */}
        {!strategy && (
          <div className="space-y-2">
            <p className="text-xs text-muted-foreground">
              No merge strategy annotation found. Defaults to <code className="font-mono text-primary">full_refresh</code>.
            </p>
            <p className="text-[10px] text-muted-foreground">
              Add an annotation at the top of your pipeline code to set the strategy:
            </p>
            <div className="relative">
              <pre className="bg-muted/50 border border-border/50 p-3 text-[11px] font-mono text-muted-foreground">
                {`${commentPrefix} @merge_strategy: full_refresh`}
              </pre>
              <button
                onClick={() => handleCopy(`${commentPrefix} @merge_strategy: full_refresh`)}
                className="absolute top-2 right-2 text-muted-foreground hover:text-foreground"
                title="Copy to clipboard"
                aria-label="Copy snippet to clipboard"
              >
                {copied ? <Check className="h-3 w-3" aria-hidden="true" /> : <Copy className="h-3 w-3" aria-hidden="true" />}
              </button>
            </div>
          </div>
        )}

        {/* Strategy detected — show parsed annotations */}
        {strategy && (
          <div className="space-y-3">
            {strategyInfo && (
              <p className="text-[10px] text-muted-foreground">
                {strategyInfo.description}
              </p>
            )}

            {!isKnownStrategy && (
              <div className="flex items-center gap-1.5 text-yellow-500 text-[10px]">
                <AlertTriangle className="h-3 w-3" />
                Unknown strategy value &ldquo;{strategy}&rdquo;
              </div>
            )}

            {/* Detected annotations table */}
            <div className="space-y-1">
              <p className="text-[10px] text-muted-foreground tracking-wider">
                Detected from pipeline code:
              </p>
              <div className="bg-muted/30 border border-border/30 divide-y divide-border/20">
                {DISPLAY_FIELDS.map(({ key, label, defaultDisplay }) => {
                  const val = parsed?.[key];
                  const isDefault = val === null || val === undefined;
                  if (isDefault && !defaultDisplay) return null;
                  const display = isDefault
                    ? defaultDisplay!
                    : Array.isArray(val) ? val.join(", ") : String(val);
                  return (
                    <div key={label} className="flex px-3 py-1.5 text-[11px]">
                      <span className="font-mono text-muted-foreground w-40 shrink-0">
                        @{label}
                      </span>
                      <span className={`font-mono ${isDefault ? "text-muted-foreground/50 italic" : "text-foreground"}`}>
                        {display}{isDefault ? " (default)" : ""}
                      </span>
                    </div>
                  );
                })}
              </div>
            </div>

            {/* Missing required fields warning */}
            {missingFields.length > 0 && (
              <div className="space-y-1">
                {missingFields.map(({ label }) => (
                  <div key={label} className="flex items-center gap-1.5 text-yellow-500 text-[10px]">
                    <AlertTriangle className="h-3 w-3 shrink-0" />
                    Strategy &lsquo;{strategy}&rsquo; requires {label}
                  </div>
                ))}
              </div>
            )}

            {/* How to change */}
            <div className="space-y-2 pt-1">
              <p className="text-[10px] text-muted-foreground tracking-wider border-t border-border/30 pt-3">
                How to change
              </p>
              <p className="text-[10px] text-muted-foreground">
                Edit the annotation headers at the top of your pipeline code:
              </p>
              {currentSnippet && (
                <div className="relative">
                  <pre className="bg-muted/50 border border-border/50 p-3 text-[11px] font-mono text-muted-foreground">
                    {currentSnippet}
                  </pre>
                  <button
                    onClick={() => handleCopy(currentSnippet)}
                    className="absolute top-2 right-2 text-muted-foreground hover:text-foreground"
                    title="Copy to clipboard"
                    aria-label="Copy annotations to clipboard"
                  >
                    {copied ? <Check className="h-3 w-3" aria-hidden="true" /> : <Copy className="h-3 w-3" aria-hidden="true" />}
                  </button>
                </div>
              )}
            </div>
          </div>
        )}

        {/* Snippet generator for other strategies */}
        <div className="space-y-2 border-t border-border/30 pt-3">
          <div className="flex items-center gap-2">
            <Label htmlFor="merge-strategy-snippet" className="text-[10px] tracking-wider text-muted-foreground">
              Show snippet for:
            </Label>
            <Select value={snippetStrategy} onValueChange={(v) => setSnippetStrategy(v as MergeStrategy)}>
              <SelectTrigger id="merge-strategy-snippet" className="text-xs h-7 w-44">
                <SelectValue placeholder="Other strategies..." />
              </SelectTrigger>
              <SelectContent>
                {STRATEGIES.map((s) => (
                  <SelectItem key={s.value} value={s.value}>
                    {s.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          {snippet && (
            <div className="relative">
              <pre className="bg-muted/50 border border-border/50 p-3 text-[11px] font-mono text-muted-foreground">
                {snippet}
              </pre>
              <button
                onClick={() => handleCopy(snippet)}
                className="absolute top-2 right-2 text-muted-foreground hover:text-foreground"
                title="Copy to clipboard"
                aria-label="Copy snippet to clipboard"
              >
                {copied ? <Check className="h-3 w-3" aria-hidden="true" /> : <Copy className="h-3 w-3" aria-hidden="true" />}
              </button>
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
