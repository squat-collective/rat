"use client";

import { useMemo } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useRuns, usePipelines } from "@/hooks/use-api";
import { Loading } from "@/components/loading";
import { ErrorAlert } from "@/components/error-alert";
import { Badge } from "@/components/ui/badge";
import { cn } from "@/lib/utils";
import { STATUS_COLORS, STATUS_EMOJI } from "@/lib/constants";

export default function RunsPage() {
  const router = useRouter();
  const { data, isLoading, error } = useRuns();
  // Runs only carry pipeline_id; the list looks naked without the
  // pipeline reference. Fetch the pipelines list once and look up the
  // ns.layer.name client-side so every row links straight to its pipeline.
  const { data: pipelinesData } = usePipelines();
  const pipelinesById = useMemo(() => {
    const m = new Map<string, { namespace: string; layer: string; name: string }>();
    for (const p of pipelinesData?.pipelines ?? []) {
      m.set(p.id, { namespace: p.namespace, layer: p.layer, name: p.name });
    }
    return m;
  }, [pipelinesData]);

  if (isLoading) return <Loading text="Loading runs..." />;
  if (error) return <ErrorAlert error={error} prefix="Failed to load runs" />;

  const runs = data?.runs ?? [];

  return (
    <div className="space-y-4">
      <div>
        <h1 className="text-sm font-bold tracking-wider">Runs</h1>
        <p className="text-[10px] text-muted-foreground">
          {data?.total ?? 0} run{(data?.total ?? 0) !== 1 ? "s" : ""}
        </p>
      </div>

      <div className="overflow-auto border-2 border-border/50">
        <table className="w-full text-xs">
          <thead className="sticky top-0 bg-card/95 backdrop-blur-sm z-10">
            <tr>
              <th className="whitespace-nowrap px-3 py-2.5 text-left text-[10px] font-bold tracking-wider text-muted-foreground border-b-2 border-primary/30 w-8">
                #
              </th>
              <th className="whitespace-nowrap px-3 py-2.5 text-left text-[10px] font-bold tracking-wider text-muted-foreground border-b-2 border-primary/30">
                Status
              </th>
              <th className="whitespace-nowrap px-3 py-2.5 text-left text-[10px] font-bold tracking-wider text-muted-foreground border-b-2 border-primary/30">
                Pipeline
              </th>
              <th className="whitespace-nowrap px-3 py-2.5 text-left text-[10px] font-bold tracking-wider text-muted-foreground border-b-2 border-primary/30">
                Run ID
              </th>
              <th className="whitespace-nowrap px-3 py-2.5 text-left text-[10px] font-bold tracking-wider text-muted-foreground border-b-2 border-primary/30">
                Duration
              </th>
              <th className="whitespace-nowrap px-3 py-2.5 text-left text-[10px] font-bold tracking-wider text-muted-foreground border-b-2 border-primary/30">
                Rows
              </th>
              <th className="whitespace-nowrap px-3 py-2.5 text-left text-[10px] font-bold tracking-wider text-muted-foreground border-b-2 border-primary/30">
                Trigger
              </th>
              <th className="whitespace-nowrap px-3 py-2.5 text-left text-[10px] font-bold tracking-wider text-muted-foreground border-b-2 border-primary/30">
                Started
              </th>
            </tr>
          </thead>
          <tbody>
            {runs.map((run, i) => (
              <tr
                key={run.id}
                tabIndex={0}
                role="link"
                onClick={() => router.push(`/runs/${run.id}`)}
                onKeyDown={(e) => {
                  if (e.key === "Enter") router.push(`/runs/${run.id}`);
                }}
                className={cn(
                  "group border-t border-border/20 transition-all cursor-pointer outline-none",
                  "hover:bg-primary/5 hover:border-l-2 hover:border-l-primary",
                  "focus-visible:bg-primary/10 focus-visible:border-l-2 focus-visible:border-l-primary",
                  i % 2 === 0 ? "bg-transparent" : "bg-muted/30",
                )}
              >
                <td className="whitespace-nowrap px-3 py-2 font-mono text-[10px] text-muted-foreground/50">
                  {(i + 1).toString().padStart(2, "0")}
                </td>
                <td className="whitespace-nowrap px-3 py-2">
                  <Badge
                    variant="outline"
                    className={cn("text-[9px]", STATUS_COLORS[run.status] || "")}
                  >
                    {STATUS_EMOJI[run.status] || ""} {run.status}
                  </Badge>
                </td>
                <td className="whitespace-nowrap px-3 py-2 font-mono text-[11px]">
                  {(() => {
                    const p = pipelinesById.get(run.pipeline_id);
                    return p ? (
                      <Link
                        href={`/pipelines/${p.namespace}/${p.layer}/${p.name}`}
                        onClick={(e) => e.stopPropagation()}
                        className="hover:text-primary"
                      >
                        <span className="text-muted-foreground">{p.namespace}.</span>
                        <span className="text-muted-foreground">{p.layer}.</span>
                        {p.name}
                      </Link>
                    ) : (
                      <span className="text-muted-foreground/60">
                        {run.pipeline_id.slice(0, 8)}…
                      </span>
                    );
                  })()}
                </td>
                <td className="whitespace-nowrap px-3 py-2">
                  <Link
                    href={`/runs/${run.id}`}
                    onClick={(e) => e.stopPropagation()}
                    className="font-mono hover:text-primary"
                  >
                    {run.id.slice(0, 12)}
                  </Link>
                </td>
                <td className="whitespace-nowrap px-3 py-2 text-muted-foreground">
                  {run.duration_ms ? `${run.duration_ms}ms` : "-"}
                </td>
                <td className="whitespace-nowrap px-3 py-2 text-muted-foreground">
                  {run.rows_written ?? "-"}
                </td>
                <td className="whitespace-nowrap px-3 py-2 text-muted-foreground">
                  {run.trigger}
                </td>
                <td className="whitespace-nowrap px-3 py-2 text-muted-foreground">
                  {run.started_at
                    ? new Date(run.started_at).toLocaleString()
                    : "-"}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
        {runs.length === 0 && (
          <div className="p-8 text-center text-xs text-muted-foreground">
            No runs yet
          </div>
        )}
      </div>
    </div>
  );
}
