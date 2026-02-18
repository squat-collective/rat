"use client";

import { useState } from "react";
import Link from "next/link";
import { Badge } from "@/components/ui/badge";
import { Input } from "@/components/ui/input";
import { CreatePipelineDialog } from "@/components/create-pipeline-dialog";
import { cn } from "@/lib/utils";
import { LAYER_BADGE_COLORS } from "@/lib/constants";

interface Pipeline {
  id: string;
  namespace: string;
  layer: string;
  name: string;
  type: string;
  description: string;
}

interface PipelinesClientProps {
  pipelines: Pipeline[];
  total: number;
}

export function PipelinesClient({ pipelines, total }: PipelinesClientProps) {
  const [search, setSearch] = useState("");

  const filtered = pipelines.filter(
    (p) =>
      p.name.toLowerCase().includes(search.toLowerCase()) ||
      p.namespace.toLowerCase().includes(search.toLowerCase()),
  );

  return (
    <div className="space-y-4">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-sm font-bold tracking-wider">
            Pipelines
          </h1>
          <p className="text-[10px] text-muted-foreground">
            {total} pipeline{total !== 1 ? "s" : ""}
          </p>
        </div>
        <CreatePipelineDialog />
      </div>

      {/* Search */}
      <Input
        placeholder="Search pipelines..."
        value={search}
        onChange={(e) => setSearch(e.target.value)}
        className="text-xs max-w-sm"
      />

      {/* Table */}
      <div className="overflow-auto border-2 border-border/50">
        <table className="w-full text-xs">
          <thead className="sticky top-0 bg-card/95 backdrop-blur-sm z-10">
            <tr>
              <th className="whitespace-nowrap px-3 py-2.5 text-left text-[10px] font-bold tracking-wider text-muted-foreground border-b-2 border-primary/30 w-8">
                #
              </th>
              <th className="whitespace-nowrap px-3 py-2.5 text-left text-[10px] font-bold tracking-wider text-muted-foreground border-b-2 border-primary/30">
                Name
              </th>
              <th className="whitespace-nowrap px-3 py-2.5 text-left text-[10px] font-bold tracking-wider text-muted-foreground border-b-2 border-primary/30">
                Layer
              </th>
              <th className="whitespace-nowrap px-3 py-2.5 text-left text-[10px] font-bold tracking-wider text-muted-foreground border-b-2 border-primary/30">
                Namespace
              </th>
              <th className="whitespace-nowrap px-3 py-2.5 text-left text-[10px] font-bold tracking-wider text-muted-foreground border-b-2 border-primary/30">
                Type
              </th>
              <th className="whitespace-nowrap px-3 py-2.5 text-left text-[10px] font-bold tracking-wider text-muted-foreground border-b-2 border-primary/30">
                Description
              </th>
            </tr>
          </thead>
          <tbody>
            {filtered.map((p, i) => (
              <tr
                key={p.id}
                className={cn(
                  "group border-t border-border/20 transition-all cursor-pointer",
                  "hover:bg-primary/5 hover:border-l-2 hover:border-l-primary",
                  i % 2 === 0 ? "bg-transparent" : "bg-muted/30",
                )}
              >
                <td className="whitespace-nowrap px-3 py-2 font-mono text-[10px] text-muted-foreground/50">
                  {(i + 1).toString().padStart(2, "0")}
                </td>
                <td className="whitespace-nowrap px-3 py-2">
                  <Link
                    href={`/pipelines/${p.namespace}/${p.layer}/${p.name}`}
                    className="font-medium hover:text-primary"
                  >
                    {p.name}
                  </Link>
                </td>
                <td className="whitespace-nowrap px-3 py-2">
                  <Badge
                    variant="outline"
                    className={cn(
                      "text-[9px]",
                      LAYER_BADGE_COLORS[p.layer] || "",
                    )}
                  >
                    {p.layer}
                  </Badge>
                </td>
                <td className="whitespace-nowrap px-3 py-2">
                  <Badge variant="secondary" className="text-[9px]">
                    {p.namespace}
                  </Badge>
                </td>
                <td className="whitespace-nowrap px-3 py-2 text-muted-foreground">
                  {p.type === "sql" ? "\u{1F4DD}" : "\u{1F40D}"} {p.type}
                </td>
                <td className="px-3 py-2 text-muted-foreground truncate max-w-[300px]">
                  {p.description || "-"}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
        {filtered.length === 0 && (
          <div className="p-8 text-center text-xs text-muted-foreground">
            {search ? "No pipelines match your search" : "No pipelines yet"}
          </div>
        )}
      </div>
    </div>
  );
}
