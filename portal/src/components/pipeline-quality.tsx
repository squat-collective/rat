"use client";

import { useCallback } from "react";
import type { QualityTestResult, PreviewColumn } from "@squat-collective/rat-client";
import {
  useQualityTests,
  useRunQualityTests,
  usePreviewQualityTest,
} from "@/hooks/use-api";
import { useScreenGlitch } from "@/components/screen-glitch";
import { ErrorAlert } from "@/components/error-alert";
import { DataTable } from "@/components/data-table";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Play, Loader2 } from "lucide-react";

interface PipelineQualityProps {
  ns: string;
  layer: string;
  name: string;
}

function severityBadge(severity: string) {
  if (severity === "error") {
    return (
      <Badge variant="outline" className="text-[9px] border-destructive/50 text-destructive">
        error
      </Badge>
    );
  }
  return (
    <Badge variant="outline" className="text-[9px] border-yellow-500/50 text-yellow-500">
      warn
    </Badge>
  );
}

function publishedBadge(published: boolean) {
  if (published) {
    return (
      <Badge variant="outline" className="text-[9px] border-green-500/50 text-green-400">
        published
      </Badge>
    );
  }
  return (
    <Badge variant="outline" className="text-[9px] border-yellow-500/50 text-yellow-500">
      draft
    </Badge>
  );
}

function statusBadge(result: QualityTestResult) {
  const status = result.status;
  if (status === "passed") {
    return (
      <span className="flex items-center gap-1 text-[10px] text-green-400">
        {"\u2705"} passed <span className="text-muted-foreground">{result.duration_ms}ms</span>
      </span>
    );
  }
  if (status === "warned") {
    return (
      <span className="flex items-center gap-1 text-[10px] text-yellow-400">
        {"\u26A0\uFE0F"} warned ({result.value} rows) <span className="text-muted-foreground">{result.duration_ms}ms</span>
      </span>
    );
  }
  if (status === "failed") {
    return (
      <span className="flex items-center gap-1 text-[10px] text-destructive">
        {"\u274C"} failed ({result.value} rows) <span className="text-muted-foreground">{result.duration_ms}ms</span>
      </span>
    );
  }
  return (
    <span className="flex items-center gap-1 text-[10px] text-destructive">
      {"\u274C"} error <span className="text-muted-foreground">{result.duration_ms}ms</span>
    </span>
  );
}

export function PipelineQuality({ ns, layer, name }: PipelineQualityProps) {
  const { data, isLoading, error } = useQualityTests(ns, layer, name);
  const { runTests, running, results } = useRunQualityTests(ns, layer, name);
  const { preview: previewTest, loading: previewLoading, results: previewResults, errors: previewErrors } = usePreviewQualityTest(ns, layer, name);
  const { triggerGlitch, GlitchOverlay } = useScreenGlitch();

  const tests = data?.tests ?? [];
  const publishedCount = tests.filter((t) => t.published).length;
  const draftCount = tests.length - publishedCount;
  const resultsByName = new Map(
    (results?.results ?? []).map((r) => [r.name, r]),
  );

  const handleRunAll = useCallback(async () => {
    try {
      await runTests();
    } catch (e) {
      console.error("Failed to run quality tests:", e);
      triggerGlitch();
    }
  }, [runTests, triggerGlitch]);

  return (
    <div className="space-y-4">
      <GlitchOverlay />

      {/* Header */}
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2">
          <h2 className="text-xs font-bold tracking-wider text-muted-foreground">
            Quality Tests
          </h2>
          {tests.length > 0 && (
            <span className="text-[10px] text-muted-foreground">
              {publishedCount} published, {draftCount} draft
            </span>
          )}
        </div>
        <Button
          variant="ghost"
          size="sm"
          className="h-6 text-[10px] gap-1"
          onClick={handleRunAll}
          disabled={running || tests.length === 0}
        >
          <Play className="h-3 w-3" />
          {running ? "Running..." : "Run All"}
        </Button>
      </div>

      {/* Loading */}
      {isLoading && (
        <p className="text-[10px] text-muted-foreground">Loading quality tests...</p>
      )}

      {/* Error */}
      {error && <ErrorAlert error={error} prefix="Failed to load quality tests" />}

      {/* Empty state */}
      {!isLoading && tests.length === 0 && (
        <div className="brutal-card bg-card p-4">
          <p className="text-[10px] text-muted-foreground">
            No quality tests yet. Right-click in the Code editor to add one.
          </p>
        </div>
      )}

      {/* Test cards (read-only) */}
      {tests.map((test) => {
        const result = resultsByName.get(test.name);
        const testPreview = previewResults[test.name];
        const testError = previewErrors[test.name];
        const isPreviewLoading = previewLoading === test.name;

        return (
          <div key={test.name} className="brutal-card bg-card p-4 space-y-3">
            {/* Card header */}
            <div className="flex items-center gap-2">
              <span className="text-xs font-bold font-mono">{test.name}</span>
              {severityBadge(test.severity)}
              {publishedBadge(test.published)}
              {result && statusBadge(result)}
              <div className="flex-1" />
              <Button
                variant="ghost"
                size="sm"
                className="h-6 text-[10px] gap-1"
                disabled={isPreviewLoading}
                onClick={() => previewTest(test.name, test.sql)}
              >
                {isPreviewLoading ? (
                  <Loader2 className="h-3 w-3 animate-spin" />
                ) : (
                  <Play className="h-3 w-3" />
                )}
                {isPreviewLoading ? "Running..." : "Preview"}
              </Button>
            </div>

            {/* Tags */}
            {test.tags && test.tags.length > 0 && (
              <div className="flex flex-wrap gap-1">
                {test.tags.map((tag) => (
                  <Badge
                    key={tag}
                    variant="outline"
                    className="text-[9px] border-primary/30 text-primary/80"
                  >
                    {tag}
                  </Badge>
                ))}
              </div>
            )}

            {/* Description */}
            {test.description && (
              <p className="text-[10px] text-muted-foreground">{test.description}</p>
            )}

            {/* SQL (read-only) */}
            <pre className="w-full font-mono text-[11px] bg-background border border-border/50 p-3 overflow-x-auto whitespace-pre-wrap">
              {test.sql}
            </pre>

            {/* Preview results */}
            {testError && !testPreview?.rows?.length && (
              <div className="text-xs text-destructive font-mono p-3 bg-destructive/5 border border-destructive/20">
                {testError}
              </div>
            )}
            {testPreview && !testError && (
              <div className="space-y-2">
                {(testPreview.rows?.length ?? 0) === 0 ? (
                  <Badge variant="outline" className="text-[10px] border-green-500/50 text-green-400">
                    ✅ PASS — 0 violations
                  </Badge>
                ) : (
                  <>
                    <Badge variant="outline" className={`text-[10px] ${
                      test.severity === "warn"
                        ? "border-yellow-500/50 text-yellow-400"
                        : "border-destructive/50 text-destructive"
                    }`}>
                      {test.severity === "warn" ? "⚠️" : "❌"} {test.severity === "warn" ? "WARN" : "FAIL"} — {testPreview.rows.length} violation(s)
                    </Badge>
                    <div className="border border-border/50 max-h-[200px] overflow-auto">
                      <DataTable
                        columns={testPreview.columns?.map((c: PreviewColumn) => c.name) ?? []}
                        rows={testPreview.rows}
                        maxHeight="200px"
                      />
                    </div>
                  </>
                )}
              </div>
            )}

            {/* Remediation — shown when test fails */}
            {test.remediation && result && result.status !== "passed" && (
              <div className="text-[10px] bg-muted/30 border border-border/30 p-3 space-y-1">
                <span className="font-bold tracking-wider text-muted-foreground">What to do</span>
                <p className="text-muted-foreground">{test.remediation}</p>
              </div>
            )}
          </div>
        );
      })}

      {/* Results summary */}
      {results && (
        <div className="brutal-card bg-card p-4">
          <div className="flex items-center gap-4 text-[10px]">
            <span className="font-bold tracking-wider text-muted-foreground">Results</span>
            <span className="text-green-400">{"\u2705"} {results.passed} passed</span>
            <span className="text-destructive">{"\u274C"} {results.failed} failed</span>
            <span className="text-muted-foreground">
              Total: {results.results.reduce((sum, r) => sum + r.duration_ms, 0)}ms
            </span>
          </div>
        </div>
      )}
    </div>
  );
}
