"use client";

import { useEffect, useState } from "react";
import { useLineage, useNamespaces } from "@/hooks/use-api";
import { Loading } from "@/components/loading";
import { ErrorAlert } from "@/components/error-alert";
import { LineageDag } from "@/components/dag/lineage-dag";
import { useScreenGlitch } from "@/components/screen-glitch";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";

export default function LineagePage() {
  const [namespace, setNamespace] = useState<string | undefined>(undefined);
  const { data: nsData } = useNamespaces();
  const { data, isLoading, error } = useLineage(namespace);
  const { triggerGlitch, GlitchOverlay } = useScreenGlitch();

  useEffect(() => {
    if (error) triggerGlitch();
  }, [error, triggerGlitch]);

  if (isLoading) return <Loading text="Building lineage..." />;

  return (
    <div className="space-y-4">
      <GlitchOverlay />

      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-sm font-bold tracking-wider">
            Lineage
          </h1>
          <p className="text-[10px] text-muted-foreground">
            Pipeline dependency graph
          </p>
        </div>

        {/* Namespace filter */}
        <Select
          value={namespace ?? "__all__"}
          onValueChange={(v) => setNamespace(v === "__all__" ? undefined : v)}
        >
          <SelectTrigger className="w-[180px] text-xs h-8">
            <SelectValue placeholder="All namespaces" />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="__all__" className="text-xs">
              All namespaces
            </SelectItem>
            {(nsData?.namespaces ?? []).map((ns) => (
              <SelectItem key={ns.name} value={ns.name} className="text-xs">
                {ns.name}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>

      {/* Error state */}
      {error && <ErrorAlert error={error} prefix="Failed to load lineage" />}

      {/* Graph or empty state */}
      {data && data.nodes.length > 0 ? (
        <LineageDag graph={data} />
      ) : (
        !error && (
          <div className="border-2 border-border p-8 text-center text-xs text-muted-foreground">
            No pipelines found. Create your first pipeline to see the lineage graph.
          </div>
        )
      )}
    </div>
  );
}
