"use client";

import { memo } from "react";
import { Handle, Position } from "@xyflow/react";
import type { NodeProps } from "@xyflow/react";
import { Badge } from "@/components/ui/badge";
import { LAYER_BADGE_COLORS, STATUS_COLORS, STATUS_EMOJI } from "@/lib/constants";
import type { LineageNode } from "@squat-collective/rat-client";

type PipelineNodeData = LineageNode & { label?: string };

function PipelineNodeComponent({ data }: NodeProps) {
  const d = data as unknown as PipelineNodeData;
  const layerColor = LAYER_BADGE_COLORS[d.layer ?? ""] ?? "";
  const runStatus = d.latest_run?.status ?? "";
  const statusColor = STATUS_COLORS[runStatus] ?? "";
  const statusEmoji = STATUS_EMOJI[runStatus] ?? "";

  return (
    <div className="brutal-card border-2 border-border bg-card px-3 py-2 min-w-[200px] cursor-pointer hover:border-primary/50 transition-colors">
      <Handle type="target" position={Position.Left} className="!bg-primary !w-2 !h-2 !border-0" />

      <div className="flex items-center gap-2 mb-1">
        <span className="text-xs font-bold text-primary truncate">{d.name}</span>
        {d.layer && (
          <Badge className={`text-[8px] px-1 py-0 ${layerColor}`}>
            {d.layer}
          </Badge>
        )}
      </div>

      {d.latest_run && (
        <div className="flex items-center gap-1 mb-0.5">
          <span className="text-[10px]">{statusEmoji}</span>
          <Badge className={`text-[8px] px-1 py-0 ${statusColor}`}>
            {runStatus}
          </Badge>
        </div>
      )}

      {d.quality && d.quality.total > 0 && (
        <div className="text-[9px] text-muted-foreground">
          Tests: {d.quality.total}
        </div>
      )}

      <Handle type="source" position={Position.Right} className="!bg-primary !w-2 !h-2 !border-0" />
    </div>
  );
}

export const PipelineNode = memo(PipelineNodeComponent);
