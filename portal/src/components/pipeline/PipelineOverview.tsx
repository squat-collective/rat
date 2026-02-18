"use client";

import { useState, useCallback, useMemo } from "react";
import Link from "next/link";
import type { Pipeline, PipelineVersion } from "@squat-collective/rat-client";
import { useFileTree, useFileContent } from "@/hooks/use-api";
import { useApiClient } from "@/providers/api-provider";
import { useSWRConfig } from "swr";
import { KEYS } from "@/lib/cache-keys";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { extractLandingZones } from "@/lib/pipeline-utils";
import { History, RotateCcw } from "lucide-react";
import { VersionTestDiff } from "@/components/version-test-diff";

interface PipelineOverviewProps {
  pipeline: Pipeline;
  versions: PipelineVersion[];
  versionsLoading: boolean;
  onVersionsRefresh: () => Promise<void>;
  triggerGlitch: () => void;
}

export function PipelineOverview({
  pipeline,
  versions,
  versionsLoading,
  onVersionsRefresh,
  triggerGlitch,
}: PipelineOverviewProps) {
  const api = useApiClient();
  const { mutate } = useSWRConfig();

  const pipelinePrefix = `${pipeline.namespace}/pipelines/${pipeline.layer}/${pipeline.name}/`;

  const { data: filesData } = useFileTree(pipelinePrefix.slice(0, -1));

  // Read pipeline source to extract landing zones
  const sourceFile = `${pipelinePrefix}pipeline.${pipeline.type === "python" ? "py" : "sql"}`;
  const { data: sourceData } = useFileContent(sourceFile);
  const landingZones = useMemo(
    () => (sourceData?.content ? extractLandingZones(sourceData.content) : []),
    [sourceData?.content],
  );

  // Rollback
  const [rollingBack, setRollingBack] = useState<number | null>(null);

  const handleRollback = useCallback(async (versionNumber: number) => {
    setRollingBack(versionNumber);
    try {
      await api.pipelines.rollback(pipeline.namespace, pipeline.layer, pipeline.name, versionNumber);
      await mutate(KEYS.match.pipelines);
      await mutate(KEYS.match.lineage);
      await onVersionsRefresh();
    } catch (e) {
      console.error("Failed to rollback pipeline version:", e);
      triggerGlitch();
    } finally {
      setRollingBack(null);
    }
  }, [api, pipeline.namespace, pipeline.layer, pipeline.name, mutate, triggerGlitch, onVersionsRefresh]);

  return (
    <div className="space-y-4">
      {/* Pipeline info */}
      <div className="brutal-card bg-card p-4 space-y-3">
        <h2 className="text-xs font-bold tracking-wider text-muted-foreground">
          Details
        </h2>

        {pipeline.description && (
          <p className="text-xs text-muted-foreground">
            {pipeline.description}
          </p>
        )}

        <div className="grid grid-cols-2 lg:grid-cols-4 gap-3">
          <div>
            <p className="text-[10px] tracking-wider text-muted-foreground">
              Namespace
            </p>
            <p className="text-xs font-medium mt-0.5">
              {pipeline.namespace}
            </p>
          </div>
          <div>
            <p className="text-[10px] tracking-wider text-muted-foreground">
              Layer
            </p>
            <p className="text-xs font-medium mt-0.5">{pipeline.layer}</p>
          </div>
          <div>
            <p className="text-[10px] tracking-wider text-muted-foreground">
              Type
            </p>
            <p className="text-xs font-medium mt-0.5">{pipeline.type}</p>
          </div>
          <div>
            <p className="text-[10px] tracking-wider text-muted-foreground">
              Owner
            </p>
            <p className="text-xs font-medium mt-0.5">
              {pipeline.owner ?? "unassigned"}
            </p>
          </div>
          <div>
            <p className="text-[10px] tracking-wider text-muted-foreground">
              S3 Path
            </p>
            <p className="text-xs font-mono mt-0.5 truncate">
              {pipeline.s3_path}
            </p>
          </div>
          <div>
            <p className="text-[10px] tracking-wider text-muted-foreground">
              Created
            </p>
            <p className="text-xs mt-0.5">
              {new Date(pipeline.created_at).toLocaleDateString()}
            </p>
          </div>
          <div>
            <p className="text-[10px] tracking-wider text-muted-foreground">
              Published
            </p>
            <p className="text-xs mt-0.5">
              {pipeline.published_at
                ? new Date(pipeline.published_at).toLocaleString()
                : "Never"}
            </p>
          </div>
          <div>
            <p className="text-[10px] tracking-wider text-muted-foreground">
              Files
            </p>
            <p className="text-xs mt-0.5">
              {filesData?.files?.length ?? 0} files
            </p>
          </div>
          {landingZones.length > 0 && (
            <div>
              <p className="text-[10px] tracking-wider text-muted-foreground">
                Landing Zones
              </p>
              <div className="flex flex-wrap gap-1 mt-0.5">
                {landingZones.map((zone) => (
                  <Link key={zone} href={`/landing?zone=${encodeURIComponent(zone)}`}>
                    <Badge variant="outline" className="text-[9px] cursor-pointer hover:bg-muted">
                      {zone}
                    </Badge>
                  </Link>
                ))}
              </div>
            </div>
          )}
        </div>
      </div>

      {/* Version History */}
      <div className="brutal-card bg-card p-4 space-y-3">
        <div className="flex items-center gap-2">
          <History className="h-3 w-3 text-muted-foreground" />
          <h2 className="text-xs font-bold tracking-wider text-muted-foreground">
            Version History
          </h2>
        </div>

        {versionsLoading ? (
          <p className="text-[10px] text-muted-foreground">Loading versions...</p>
        ) : versions.length === 0 ? (
          <p className="text-[10px] text-muted-foreground">No versions published yet.</p>
        ) : (
          <div className="space-y-2">
            {versions.slice(0, 10).map((v, idx) => (
              <div
                key={v.id}
                className="border border-border/30 px-3 py-2"
              >
                <div className="flex items-center justify-between">
                  <div className="flex items-center gap-3 min-w-0">
                    <Badge variant="outline" className="text-[9px] shrink-0">
                      v{v.version_number}
                    </Badge>
                    <span className="text-xs truncate">
                      {v.message || <span className="text-muted-foreground italic">No message</span>}
                    </span>
                    <span className="text-[10px] text-muted-foreground shrink-0">
                      {new Date(v.created_at).toLocaleString()}
                    </span>
                  </div>
                  {v.version_number !== versions[0]?.version_number && (
                    <Button
                      size="sm"
                      variant="ghost"
                      className="h-6 text-[10px] gap-1 shrink-0"
                      disabled={rollingBack === v.version_number}
                      onClick={() => handleRollback(v.version_number)}
                    >
                      <RotateCcw className="h-3 w-3" />
                      {rollingBack === v.version_number ? "Rolling back..." : "Rollback"}
                    </Button>
                  )}
                </div>
                <VersionTestDiff
                  current={v}
                  previous={versions[idx + 1]}
                />
              </div>
            ))}
            {versions.length > 10 && (
              <p className="text-[10px] text-muted-foreground">
                Showing 10 of {versions.length} versions.
              </p>
            )}
          </div>
        )}
      </div>
    </div>
  );
}
