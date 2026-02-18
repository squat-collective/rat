"use client";

import { useRouter } from "next/navigation";
import Link from "next/link";
import type { Pipeline, PipelineVersion } from "@squat-collective/rat-client";
import { useCreateRun } from "@/hooks/use-api";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import { LAYER_BADGE_COLORS } from "@/lib/constants";
import { ArrowLeft, Play } from "lucide-react";

interface PipelineHeaderProps {
  pipeline: Pipeline;
  versions: PipelineVersion[];
  triggerGlitch: () => void;
}

export function PipelineHeader({
  pipeline,
  versions,
  triggerGlitch,
}: PipelineHeaderProps) {
  const router = useRouter();
  const { createRun, creating: creatingRun } = useCreateRun(
    pipeline.namespace,
    pipeline.layer,
    pipeline.name,
  );

  return (
    <div>
      <Link
        href="/pipelines"
        className="text-[10px] text-muted-foreground hover:text-primary flex items-center gap-1 mb-2"
      >
        <ArrowLeft className="h-3 w-3" /> Back to pipelines
      </Link>
      <div className="flex items-center gap-3">
        <h1 className="text-sm font-bold tracking-wider">
          {pipeline.name}
        </h1>
        <Badge
          variant="outline"
          className={cn(
            "text-[9px]",
            LAYER_BADGE_COLORS[pipeline.layer] || "",
          )}
        >
          {pipeline.layer}
        </Badge>
        <Badge variant="secondary" className="text-[9px]">
          {pipeline.namespace}
        </Badge>
        <Badge variant="secondary" className="text-[9px]">
          {pipeline.type === "sql" ? "\u{1F4DD}" : "\u{1F40D}"}{" "}
          {pipeline.type}
        </Badge>
        {pipeline.draft_dirty && (
          <Badge variant="outline" className="text-[9px] border-yellow-500 text-yellow-500">
            Draft
          </Badge>
        )}
        {versions.length > 0 && (
          <Badge variant="outline" className="text-[9px] border-primary/50 text-primary">
            v{versions[0].version_number}
          </Badge>
        )}
        <Button
          size="sm"
          variant="ghost"
          className="ml-auto h-7 text-[10px] gap-1"
          disabled={creatingRun}
          onClick={async () => {
            try {
              const result = await createRun();
              router.push(`/runs/${result.run_id}`);
            } catch (e) {
              console.error("Failed to create pipeline run:", e);
              triggerGlitch();
            }
          }}
        >
          <Play className="h-3 w-3" />
          {creatingRun ? "Running..." : "Run"}
        </Button>
      </div>
    </div>
  );
}
