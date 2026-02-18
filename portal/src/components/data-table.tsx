"use client";

import { useMemo } from "react";
import { cn } from "@/lib/utils";

type DataTableProps = {
  columns: string[];
  rows: Record<string, unknown>[];
  className?: string;
  maxHeight?: string;
  caption?: string;
};

export function DataTable({
  columns,
  rows,
  className,
  maxHeight = "500px",
  caption,
}: DataTableProps) {
  const columnDefs = useMemo(
    () =>
      columns.map((col) => ({
        key: col,
        header: (
          <th
            key={col}
            className="whitespace-nowrap px-3 py-2.5 text-left text-[10px] font-bold tracking-wider text-muted-foreground border-b-2 border-primary/30"
          >
            {col}
          </th>
        ),
        renderCell: (row: Record<string, unknown>) => {
          const cell = row[col];
          return (
            <td
              key={col}
              className="whitespace-nowrap px-3 py-2 font-mono text-[11px] group-hover:text-foreground transition-colors"
            >
              {cell === null || cell === undefined ? (
                <span className="text-destructive/40 italic">NULL</span>
              ) : typeof cell === "number" ? (
                <span className="text-neon-cyan">{String(cell)}</span>
              ) : typeof cell === "boolean" ? (
                <span
                  className={
                    cell ? "text-primary" : "text-destructive/60"
                  }
                >
                  {String(cell)}
                </span>
              ) : (
                String(cell)
              )}
            </td>
          );
        },
      })),
    [columns],
  );

  if (!columns.length) {
    return (
      <p className="text-xs text-muted-foreground font-mono">{"// no data"}</p>
    );
  }

  return (
    <div
      className={cn("overflow-auto border-2 border-border/50", className)}
      style={{ maxHeight }}
    >
      <table className="w-full text-xs">
        {caption && <caption className="sr-only">{caption}</caption>}
        <thead className="sticky top-0 bg-card/95 backdrop-blur-sm z-10">
          <tr>
            <th className="whitespace-nowrap px-3 py-2.5 text-left text-[10px] font-bold tracking-wider text-primary/70 border-b-2 border-primary/30 w-8">
              #
            </th>
            {columnDefs.map((col) => col.header)}
          </tr>
        </thead>
        <tbody>
          {rows.map((row, i) => (
            <tr
              key={i}
              className={cn(
                "group border-t border-border/20 transition-all",
                "hover:bg-primary/5 hover:border-l-2 hover:border-l-primary",
                i % 2 === 0 ? "bg-transparent" : "bg-muted/30",
              )}
            >
              <td className="whitespace-nowrap px-3 py-2 font-mono text-[10px] text-muted-foreground/50 select-none">
                {(i + 1).toString().padStart(2, "0")}
              </td>
              {columnDefs.map((col) => col.renderCell(row))}
            </tr>
          ))}
        </tbody>
      </table>
      <div className="sticky bottom-0 border-t-2 border-primary/20 bg-card/95 backdrop-blur-sm px-3 py-1.5">
        <span className="text-[10px] text-muted-foreground font-mono">
          {rows.length} row{rows.length !== 1 ? "s" : ""} returned
        </span>
      </div>
    </div>
  );
}
