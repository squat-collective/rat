"use client";

import { useRunnerPlugins } from "@/hooks/use-api";
import { useScreenGlitch } from "@/components/screen-glitch";
import { Loading } from "@/components/loading";
import { ErrorAlert } from "@/components/error-alert";
import { Badge } from "@/components/ui/badge";
import type { RunnerPlugin } from "@squat-collective/rat-client";

/** Human-readable labels for runner extension groups. */
const GROUP_LABELS: Record<string, string> = {
  "rat.strategies": "Merge Strategies",
  "rat.pipeline_types": "Pipeline Types",
  "rat.jinja_helpers": "Jinja Helpers",
  "rat.hooks": "Hooks",
  "rat.sources": "Sources",
};

function groupLabel(group: string): string {
  return GROUP_LABELS[group] ?? group;
}

function groupPlugins(
  plugins: RunnerPlugin[],
): Map<string, RunnerPlugin[]> {
  const grouped = new Map<string, RunnerPlugin[]>();
  for (const p of plugins) {
    const list = grouped.get(p.group) ?? [];
    list.push(p);
    grouped.set(p.group, list);
  }
  return grouped;
}

export function RunnerPluginList() {
  const { data: plugins, error, isLoading } = useRunnerPlugins();
  const { triggerGlitch, GlitchOverlay } = useScreenGlitch();

  if (isLoading) return <Loading text="Discovering runner plugins..." />;
  if (error) {
    triggerGlitch();
    return (
      <>
        <GlitchOverlay />
        <ErrorAlert error={error} prefix="Runner plugins" />
      </>
    );
  }

  if (!plugins || plugins.length === 0) {
    return (
      <div className="brutal-card p-6 text-center">
        <p className="text-xs text-muted-foreground">
          No runner plugins discovered. Install Python packages with{" "}
          <code className="font-mono text-primary">rat.*</code> entry points.
        </p>
      </div>
    );
  }

  const grouped = groupPlugins(plugins);

  return (
    <>
      <GlitchOverlay />
      <div className="space-y-4">
        {[...grouped.entries()].map(([group, items]) => (
          <div key={group} className="brutal-card p-4 space-y-3">
            <h3 className="text-xs font-bold tracking-wider text-muted-foreground">
              {groupLabel(group)}
            </h3>
            <div className="space-y-1.5">
              {items.map((p) => (
                <div
                  key={`${p.group}-${p.name}`}
                  className="flex items-center gap-2 text-xs p-2 bg-muted/30"
                >
                  <span className="font-bold tracking-wide">{p.name}</span>
                  <Badge
                    variant="outline"
                    className="text-[10px] px-1.5 py-0 font-mono"
                  >
                    v{p.version}
                  </Badge>
                  <span className="ml-auto text-[10px] text-muted-foreground font-mono">
                    {p.package_name}
                  </span>
                </div>
              ))}
            </div>
          </div>
        ))}
      </div>
    </>
  );
}
