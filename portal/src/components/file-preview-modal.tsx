"use client";

import { useState } from "react";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { useApiClient } from "@/providers/api-provider";
import { Eye, Loader2 } from "lucide-react";
import type { QueryResult } from "@squat-collective/rat-client";

// Format readers by file extension.
const FORMAT_READERS: Record<string, string> = {
  csv: "read_csv_auto",
  tsv: "read_csv_auto",
  json: "read_json_auto",
  jsonl: "read_json_auto",
  ndjson: "read_json_auto",
  parquet: "read_parquet",
  xlsx: "st_read",
  xls: "st_read",
};

function getReader(filename: string): string {
  const ext = filename.split(".").pop()?.toLowerCase() ?? "";
  return FORMAT_READERS[ext] ?? "read_csv_auto";
}

const TYPE_COLORS: Record<string, string> = {
  INTEGER: "text-cyan-400",
  BIGINT: "text-cyan-400",
  DOUBLE: "text-cyan-400",
  FLOAT: "text-cyan-400",
  VARCHAR: "text-foreground",
  BOOLEAN: "text-green-400",
  DATE: "text-purple-400",
  TIMESTAMP: "text-purple-400",
};

interface FilePreviewModalProps {
  s3Path: string;
  filename: string;
  namespace?: string;
}

export function FilePreviewModal({
  s3Path,
  filename,
  namespace,
}: FilePreviewModalProps) {
  const api = useApiClient();
  const [open, setOpen] = useState(false);
  const [loading, setLoading] = useState(false);
  const [result, setResult] = useState<QueryResult | null>(null);
  const [error, setError] = useState<string | null>(null);

  const handleOpen = async () => {
    setOpen(true);
    setLoading(true);
    setError(null);
    setResult(null);

    const reader = getReader(filename);
    const safePath = s3Path.replace(/'/g, "''");
    const sql = `SELECT * FROM ${reader}('s3://rat/${safePath}') LIMIT 100`;

    try {
      const res = await api.query.execute({
        sql,
        namespace: namespace ?? "default",
        limit: 100,
      });
      setResult(res);
    } catch (e) {
      const msg = e instanceof Error ? e.message : "Failed to preview file";
      // Detect S3 / file-not-found errors — file was likely moved by a pipeline run
      const isFileMissing = /not found|no such|does not exist|unable to open/i.test(msg);
      setError(
        isFileMissing
          ? "File not found — it may have been moved by a pipeline run. Try refreshing the page."
          : msg,
      );
    } finally {
      setLoading(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <Button
        variant="ghost"
        size="sm"
        className="h-6 px-2 text-[10px]"
        onClick={handleOpen}
      >
        <Eye className="h-3 w-3 mr-1" /> Preview
      </Button>
      <DialogContent className="max-w-4xl max-h-[80vh] overflow-hidden flex flex-col">
        <DialogHeader>
          <DialogTitle className="text-xs font-mono">{filename}</DialogTitle>
        </DialogHeader>

        {loading && (
          <div className="flex items-center justify-center py-12">
            <Loader2 className="h-5 w-5 animate-spin text-muted-foreground" />
            <span className="ml-2 text-xs text-muted-foreground">
              Detecting schema...
            </span>
          </div>
        )}

        {error && (
          <div className="error-block px-3 py-2 text-xs text-destructive">
            {error}
          </div>
        )}

        {result && (
          <div className="flex flex-col gap-3 overflow-hidden flex-1">
            {/* Schema */}
            <div>
              <h3 className="text-[10px] font-bold tracking-wider text-muted-foreground mb-1">
                Detected Schema ({result.columns.length} columns)
              </h3>
              <div className="flex flex-wrap gap-1">
                {result.columns.map((col) => (
                  <span
                    key={col.name}
                    className="text-[10px] px-1.5 py-0.5 bg-muted border border-border/50"
                  >
                    <span className="font-medium">{col.name}</span>
                    <span
                      className={`ml-1 ${TYPE_COLORS[col.type] ?? "text-muted-foreground"}`}
                    >
                      {col.type}
                    </span>
                  </span>
                ))}
              </div>
            </div>

            {/* Data */}
            <div className="overflow-auto flex-1 border-2 border-border/50">
              <table className="w-full text-xs">
                <thead className="sticky top-0 bg-card/95 backdrop-blur-sm z-10">
                  <tr>
                    <th className="whitespace-nowrap px-2 py-1.5 text-left text-[9px] font-bold tracking-wider text-muted-foreground border-b-2 border-primary/30 w-8">
                      #
                    </th>
                    {result.columns.map((col) => (
                      <th
                        key={col.name}
                        className="whitespace-nowrap px-2 py-1.5 text-left text-[9px] font-bold tracking-wider text-muted-foreground border-b-2 border-primary/30"
                      >
                        {col.name}
                      </th>
                    ))}
                  </tr>
                </thead>
                <tbody>
                  {result.rows.map((row, i) => (
                    <tr
                      key={i}
                      className={
                        i % 2 === 0
                          ? "bg-transparent"
                          : "bg-muted/30"
                      }
                    >
                      <td className="whitespace-nowrap px-2 py-1 font-mono text-[9px] text-muted-foreground/50">
                        {(i + 1).toString().padStart(2, "0")}
                      </td>
                      {result.columns.map((col) => {
                        const val = row[col.name];
                        const isNull = val === null || val === undefined;
                        const isNum = typeof val === "number";
                        const isBool = typeof val === "boolean";
                        return (
                          <td
                            key={col.name}
                            className={`whitespace-nowrap px-2 py-1 font-mono text-[10px] ${
                              isNull
                                ? "text-red-400/60 italic"
                                : isNum
                                  ? "text-cyan-400"
                                  : isBool
                                    ? "text-green-400"
                                    : ""
                            }`}
                          >
                            {isNull ? "NULL" : String(val)}
                          </td>
                        );
                      })}
                    </tr>
                  ))}
                </tbody>
              </table>
              {result.rows.length === 0 && (
                <div className="p-4 text-center text-xs text-muted-foreground">
                  No rows
                </div>
              )}
            </div>

            <p className="text-[9px] text-muted-foreground">
              {result.total_rows} row{result.total_rows !== 1 ? "s" : ""}{" "}
              returned &middot; {result.duration_ms}ms
            </p>
          </div>
        )}
      </DialogContent>
    </Dialog>
  );
}
