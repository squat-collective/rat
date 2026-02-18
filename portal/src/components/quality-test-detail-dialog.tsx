"use client";

import { useState, useCallback } from "react";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
} from "@/components/ui/dialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Copy, Check, Maximize2, Minimize2 } from "lucide-react";

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

interface QualityTestDetailDialogProps {
  result: ParsedQualityResult | null;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}

const STATUS_CONFIG: Record<string, { emoji: string; label: string; className: string }> = {
  pass: { emoji: "\u2705", label: "PASS", className: "bg-primary/20 text-primary border-primary/50" },
  fail: { emoji: "\u274C", label: "FAIL", className: "bg-destructive/20 text-destructive border-destructive/50" },
  error: { emoji: "\u26A0\uFE0F", label: "ERROR", className: "bg-destructive/20 text-destructive border-destructive/50" },
};

const SEVERITY_CONFIG: Record<string, string> = {
  error: "bg-destructive/20 text-destructive border-destructive/50",
  warn: "bg-yellow-900/30 text-yellow-400 border-yellow-700",
};

export function QualityTestDetailDialog({
  result,
  open,
  onOpenChange,
}: QualityTestDetailDialogProps) {
  const [copied, setCopied] = useState(false);
  const [expandedViolations, setExpandedViolations] = useState(false);

  const handleCopy = useCallback(async () => {
    if (!result?.compiledSql) return;
    await navigator.clipboard.writeText(result.compiledSql);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  }, [result?.compiledSql]);

  if (!result) return null;

  const statusCfg = STATUS_CONFIG[result.status] ?? STATUS_CONFIG.error;
  const severityCls = SEVERITY_CONFIG[result.severity ?? "error"] ?? SEVERITY_CONFIG.error;

  return (
    <Dialog open={open} onOpenChange={(v) => { if (!v) setExpandedViolations(false); onOpenChange(v); }}>
      <DialogContent className="max-w-2xl max-h-[85vh] flex flex-col">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2 flex-wrap">
            <span>{statusCfg.emoji}</span>
            <span className="font-mono">{result.name}</span>
            <Badge variant="outline" className={statusCfg.className}>
              {statusCfg.label}
            </Badge>
            {result.severity && (
              <Badge variant="outline" className={severityCls}>
                {result.severity}
              </Badge>
            )}
          </DialogTitle>
          <DialogDescription>
            Quality test detail
          </DialogDescription>
        </DialogHeader>

        <ScrollArea className="flex-1 min-h-0">
          <div className="space-y-4 pr-4">
            {/* Description */}
            {result.description && (
              <div className="space-y-1">
                <p className="text-[10px] tracking-wider text-muted-foreground font-bold">
                  Description
                </p>
                <p className="text-xs">{result.description}</p>
              </div>
            )}

            {/* Metrics */}
            {(result.duration !== undefined || result.rows !== undefined) && (
              <div className="flex gap-4">
                {result.duration !== undefined && (
                  <div>
                    <p className="text-[10px] tracking-wider text-muted-foreground font-bold">
                      Duration
                    </p>
                    <p className="text-xs font-mono">{result.duration}ms</p>
                  </div>
                )}
                {result.rows !== undefined && (
                  <div>
                    <p className="text-[10px] tracking-wider text-muted-foreground font-bold">
                      Violations
                    </p>
                    <p className="text-xs font-mono">{result.rows} row(s)</p>
                  </div>
                )}
              </div>
            )}

            {/* Compiled SQL */}
            {result.compiledSql && (
              <div className="space-y-1">
                <div className="flex items-center justify-between">
                  <p className="text-[10px] tracking-wider text-muted-foreground font-bold">
                    Compiled SQL
                  </p>
                  <Button
                    variant="ghost"
                    size="sm"
                    onClick={handleCopy}
                    className="h-6 gap-1 text-[10px]"
                  >
                    {copied ? (
                      <><Check className="h-3 w-3" /> Copied</>
                    ) : (
                      <><Copy className="h-3 w-3" /> Copy</>
                    )}
                  </Button>
                </div>
                <pre className="text-[11px] font-mono bg-background border-2 border-border/50 p-3 overflow-auto max-h-[200px] whitespace-pre-wrap">
                  {result.compiledSql}
                </pre>
              </div>
            )}

            {/* Sample Violations */}
            {result.sampleRows && (
              <div className="space-y-1">
                <div className="flex items-center justify-between">
                  <p className="text-[10px] tracking-wider text-muted-foreground font-bold">
                    Sample Violations
                  </p>
                  <Button
                    variant="ghost"
                    size="sm"
                    onClick={() => setExpandedViolations((v) => !v)}
                    className="h-6 gap-1 text-[10px]"
                  >
                    {expandedViolations ? (
                      <><Minimize2 className="h-3 w-3" /> Collapse</>
                    ) : (
                      <><Maximize2 className="h-3 w-3" /> Expand</>
                    )}
                  </Button>
                </div>
                <pre className={`text-[11px] font-mono bg-background border-2 border-border/50 p-3 overflow-auto whitespace-pre ${expandedViolations ? "max-h-[60vh]" : "max-h-[200px]"}`}>
                  {result.sampleRows}
                </pre>
              </div>
            )}

            {/* Error */}
            {result.errorMessage && (
              <div className="space-y-1">
                <p className="text-[10px] tracking-wider text-muted-foreground font-bold">
                  Error
                </p>
                <div className="text-xs text-destructive bg-destructive/10 border-2 border-destructive/30 p-3">
                  {result.errorMessage}
                </div>
              </div>
            )}
          </div>
        </ScrollArea>
      </DialogContent>
    </Dialog>
  );
}
