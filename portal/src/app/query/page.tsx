"use client";

import { useState, useRef, useCallback } from "react";
import { useApiClient } from "@/providers/api-provider";
import { useQuerySchema } from "@/hooks/use-api";
import { SqlEditor, type SqlEditorHandle } from "@/components/sql-editor";
import { SchemaTree } from "@/components/schema-tree";
import { DataTable } from "@/components/data-table";
import { useScreenGlitch } from "@/components/screen-glitch";
import { Button } from "@/components/ui/button";
import { Play, AlertTriangle } from "lucide-react";
import type { QueryResult } from "@squat-collective/rat-client";

export default function QueryPage() {
  const api = useApiClient();
  const { data: schema } = useQuerySchema();
  const { triggerGlitch, GlitchOverlay } = useScreenGlitch();
  const editorRef = useRef<SqlEditorHandle>(null);

  const [sqlValue, setSqlValue] = useState("SELECT 1 as test;");
  const [result, setResult] = useState<QueryResult | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const handleExecute = useCallback(async () => {
    if (!sqlValue.trim()) return;
    setLoading(true);
    setError(null);
    setResult(null);
    try {
      const res = await api.query.execute({ sql: sqlValue });
      setResult(res);
    } catch (e) {
      console.error("Failed to execute query:", e);
      const msg = e instanceof Error ? e.message : "Query failed";
      setError(msg);
      triggerGlitch();
    } finally {
      setLoading(false);
    }
  }, [api, sqlValue, triggerGlitch]);

  const handleInsertTable = useCallback((text: string) => {
    editorRef.current?.insertAtCursor(text + " ");
  }, []);

  return (
    <div className="flex h-[calc(100vh-48px)] -m-6">
      <GlitchOverlay />

      {/* Schema sidebar */}
      <div className="w-56 border-r border-border/50 overflow-y-auto bg-card/50">
        <div className="px-2 py-2 border-b border-border/50">
          <p className="text-[10px] font-bold tracking-wider text-muted-foreground">
            Schema
          </p>
        </div>
        {schema && (
          <SchemaTree schema={schema} onInsertTable={handleInsertTable} />
        )}
      </div>

      {/* Main area */}
      <div className="flex-1 flex flex-col overflow-hidden">
        {/* SQL Editor */}
        <div className="border-b border-border/50">
          <SqlEditor
            ref={editorRef}
            value={sqlValue}
            onChange={setSqlValue}
            onExecute={handleExecute}
            schema={schema ?? undefined}
          />
          <div className="flex items-center justify-between px-3 py-1.5 bg-card/50">
            <span className="flex items-center gap-1.5 text-[10px] text-muted-foreground">
              <AlertTriangle className="h-3 w-3 text-yellow-500" />
              <span className="text-yellow-500/80">Executes real SQL</span>
              <span className="mx-1">|</span>
              Ctrl+Enter to execute
            </span>
            <Button
              size="sm"
              onClick={handleExecute}
              disabled={loading}
              className="h-7 gap-1"
            >
              <Play className="h-3 w-3" />
              {loading ? "Running..." : "Execute"}
            </Button>
          </div>
        </div>

        {/* Results */}
        <div className="flex-1 overflow-auto p-4">
          {error && (
            <div className="error-block px-4 py-3 text-xs text-destructive mb-4">
              {error}
            </div>
          )}
          {result && (
            <div className="space-y-2">
              <div className="flex items-center gap-4 text-[10px] text-muted-foreground">
                <span>
                  {result.total_rows} row{result.total_rows !== 1 ? "s" : ""}
                </span>
                <span>{result.duration_ms}ms</span>
              </div>
              <DataTable
                columns={result.columns.map((c) => c.name)}
                rows={result.rows}
                maxHeight="calc(100vh - 400px)"
              />
            </div>
          )}
          {!result && !error && !loading && (
            <div className="flex items-center justify-center h-full text-xs text-muted-foreground">
              Write SQL and press Execute
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
