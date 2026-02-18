"use client";

import Link from "next/link";
import { Play, Database, Inbox, Activity } from "lucide-react";
import { usePipelines, useRuns, useTables, useNamespaces } from "@/hooks/use-api";
import { Loading } from "@/components/loading";
import { ErrorAlert } from "@/components/error-alert";
import { Badge } from "@/components/ui/badge";
import { cn } from "@/lib/utils";
import { RAT_LOGO, STATUS_COLORS, STATUS_EMOJI, LAYER_BADGE_COLORS } from "@/lib/constants";

function formatNumber(n: number): string {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`;
  if (n >= 1_000) return `${(n / 1_000).toFixed(1)}k`;
  return n.toString();
}

function timeAgo(date: string): string {
  const diff = Date.now() - new Date(date).getTime();
  const seconds = Math.floor(diff / 1000);
  if (seconds < 60) return `${seconds}s ago`;
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `${minutes}m ago`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours}h ago`;
  return `${Math.floor(hours / 24)}d ago`;
}

export default function HomePage() {
  const { data: pipelinesData, isLoading: loadingPipelines, error: pipelinesError } = usePipelines();
  const { data: runsData, isLoading: loadingRuns, error: runsError } = useRuns();
  const { data: tablesData, isLoading: loadingTables, error: tablesError } = useTables();
  const { data: namespacesData, isLoading: loadingNamespaces, error: namespacesError } = useNamespaces();

  const isLoading = loadingPipelines || loadingRuns || loadingTables || loadingNamespaces;
  const firstError = pipelinesError || runsError || tablesError || namespacesError;

  const pipelines = pipelinesData?.pipelines ?? [];
  const runs = runsData?.runs ?? [];
  const tables = tablesData?.tables ?? [];

  // 24h runs
  const now = Date.now();
  const runs24h = runs.filter(
    (r) => now - new Date(r.created_at).getTime() < 86_400_000,
  );
  const successCount = runs24h.filter((r) => r.status === "success").length;
  const failedCount = runs24h.filter((r) => r.status === "failed").length;

  // Pipeline breakdown by layer
  const layerCounts: Record<string, number> = {};
  for (const p of pipelines) {
    layerCounts[p.layer] = (layerCounts[p.layer] || 0) + 1;
  }

  // Total row count across all tables
  const totalRows = tables.reduce((sum, t) => sum + (t.row_count || 0), 0);

  // Last 5 updated pipelines
  const recentPipelines = [...pipelines]
    .sort((a, b) => new Date(b.updated_at).getTime() - new Date(a.updated_at).getTime())
    .slice(0, 5);

  // Last 10 runs
  const recentRuns = [...runs]
    .sort((a, b) => new Date(b.created_at).getTime() - new Date(a.created_at).getTime())
    .slice(0, 10);

  if (isLoading) {
    return (
      <div className="flex items-center justify-center h-full">
        <Loading text="Loading dashboard..." />
      </div>
    );
  }

  if (firstError) {
    return (
      <div className="space-y-6">
        <div className="text-center">
          <pre
            className="glitch text-[10px] leading-[12px] text-primary neon-text font-bold select-none whitespace-pre inline-block"
            data-text={RAT_LOGO}
          >
            {RAT_LOGO}
          </pre>
        </div>
        <ErrorAlert error={firstError} prefix="Failed to load dashboard" />
      </div>
    );
  }

  return (
    <div className="space-y-6">
      {/* Row 1 — Header */}
      <div className="text-center">
        <pre
          className="glitch text-[10px] leading-[12px] text-primary neon-text font-bold select-none whitespace-pre inline-block"
          data-text={RAT_LOGO}
        >
          {RAT_LOGO}
        </pre>
        <p className="text-xs text-muted-foreground mt-2 font-mono tracking-widest">
          anyone can data!
        </p>
      </div>

      {/* Row 2 — Stat Cards */}
      <div className="grid grid-cols-2 lg:grid-cols-4 gap-3">
        <StatCard
          icon={<Play className="h-4 w-4" />}
          label="PIPELINES"
          value={pipelinesData?.total ?? 0}
          subtitle={
            Object.entries(layerCounts)
              .map(([layer, count]) => `${count} ${layer}`)
              .join(" / ") || "none"
          }
        />
        <StatCard
          icon={<Database className="h-4 w-4" />}
          label="TABLES"
          value={tablesData?.total ?? 0}
          subtitle={`${formatNumber(totalRows)} total rows`}
        />
        <StatCard
          icon={<Inbox className="h-4 w-4" />}
          label="NAMESPACES"
          value={namespacesData?.total ?? 0}
        />
        <StatCard
          icon={<Activity className="h-4 w-4" />}
          label="RUNS (24H)"
          value={runs24h.length}
          subtitle={`${successCount} success / ${failedCount} failed`}
        />
      </div>

      {/* Row 3 — Detail Panels */}
      <div className="grid grid-cols-1 lg:grid-cols-2 gap-3">
        {/* Left: Pipeline Breakdown */}
        <div className="brutal-card bg-card p-4 space-y-4">
          <h2 className="text-[10px] font-bold tracking-wider text-muted-foreground">
            PIPELINE BREAKDOWN
          </h2>

          {/* Layer counts */}
          <div className="flex gap-2 flex-wrap">
            {(["bronze", "silver", "gold"] as const).map((layer) => (
              <Badge
                key={layer}
                variant="outline"
                className={cn("text-[9px]", LAYER_BADGE_COLORS[layer] || "")}
              >
                {layer}: {layerCounts[layer] || 0}
              </Badge>
            ))}
          </div>

          {/* Recent pipelines */}
          {recentPipelines.length > 0 ? (
            <div className="space-y-1.5">
              <p className="text-[9px] text-muted-foreground tracking-wider">
                RECENTLY UPDATED
              </p>
              {recentPipelines.map((p) => (
                <div
                  key={p.id}
                  className="flex items-center gap-2 text-xs py-1 border-b border-border/20 last:border-0"
                >
                  <Badge
                    variant="outline"
                    className={cn("text-[8px] px-1", LAYER_BADGE_COLORS[p.layer] || "")}
                  >
                    {p.layer}
                  </Badge>
                  <span className="font-mono truncate">{p.name}</span>
                  <span className="text-[9px] text-muted-foreground ml-auto shrink-0">
                    {p.type}
                  </span>
                </div>
              ))}
            </div>
          ) : (
            <p className="text-xs text-muted-foreground">No pipelines yet</p>
          )}
        </div>

        {/* Right: Recent Runs */}
        <div className="brutal-card bg-card p-4 space-y-4">
          <h2 className="text-[10px] font-bold tracking-wider text-muted-foreground">
            RECENT RUNS
          </h2>

          {recentRuns.length > 0 ? (
            <div className="overflow-auto">
              <table className="w-full text-xs">
                <thead>
                  <tr>
                    <th className="text-left text-[9px] font-bold tracking-wider text-muted-foreground pb-2 pr-2">
                      Status
                    </th>
                    <th className="text-left text-[9px] font-bold tracking-wider text-muted-foreground pb-2 pr-2">
                      Run ID
                    </th>
                    <th className="text-left text-[9px] font-bold tracking-wider text-muted-foreground pb-2 pr-2">
                      Duration
                    </th>
                    <th className="text-left text-[9px] font-bold tracking-wider text-muted-foreground pb-2 pr-2">
                      Trigger
                    </th>
                    <th className="text-right text-[9px] font-bold tracking-wider text-muted-foreground pb-2">
                      Time
                    </th>
                  </tr>
                </thead>
                <tbody>
                  {recentRuns.map((run, i) => (
                    <tr
                      key={run.id}
                      className={cn(
                        "border-t border-border/20",
                        i % 2 === 0 ? "bg-transparent" : "bg-muted/30",
                      )}
                    >
                      <td className="whitespace-nowrap py-1.5 pr-2">
                        <Badge
                          variant="outline"
                          className={cn("text-[8px]", STATUS_COLORS[run.status] || "")}
                        >
                          {STATUS_EMOJI[run.status] || ""} {run.status}
                        </Badge>
                      </td>
                      <td className="whitespace-nowrap py-1.5 pr-2">
                        <Link
                          href={`/runs/${run.id}`}
                          className="font-mono text-[10px] hover:text-primary transition-colors"
                        >
                          {run.id.slice(0, 12)}
                        </Link>
                      </td>
                      <td className="whitespace-nowrap py-1.5 pr-2 text-muted-foreground text-[10px]">
                        {run.duration_ms ? `${run.duration_ms}ms` : "-"}
                      </td>
                      <td className="whitespace-nowrap py-1.5 pr-2 text-muted-foreground text-[10px]">
                        {run.trigger}
                      </td>
                      <td className="whitespace-nowrap py-1.5 text-right text-muted-foreground text-[10px]">
                        {timeAgo(run.created_at)}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          ) : (
            <p className="text-xs text-muted-foreground">No runs yet</p>
          )}
        </div>
      </div>
    </div>
  );
}

function StatCard({
  icon,
  label,
  value,
  subtitle,
}: {
  icon: React.ReactNode;
  label: string;
  value: number;
  subtitle?: string;
}) {
  return (
    <div className="brutal-card bg-card p-4 space-y-2">
      <div className="flex items-center gap-2 text-muted-foreground">
        {icon}
        <span className="text-[10px] font-bold tracking-widest">{label}</span>
      </div>
      <p className="text-2xl font-bold neon-text text-primary font-mono">
        {formatNumber(value)}
      </p>
      {subtitle && (
        <p className="text-[10px] text-muted-foreground">{subtitle}</p>
      )}
    </div>
  );
}
