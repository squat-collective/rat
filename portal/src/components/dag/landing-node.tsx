"use client";

import { memo } from "react";
import { Handle, Position } from "@xyflow/react";
import type { NodeProps } from "@xyflow/react";
import { Inbox } from "lucide-react";
import type { LineageNode } from "@squat-collective/rat-client";

type LandingNodeData = LineageNode & { label?: string };

function LandingNodeComponent({ data }: NodeProps) {
  const d = data as unknown as LandingNodeData;

  return (
    <div className="brutal-card border-2 border-amber-700/50 bg-amber-900/10 px-3 py-2 min-w-[160px] cursor-pointer hover:border-amber-500/50 transition-colors">
      <div className="flex items-center gap-2 mb-1">
        <Inbox className="w-3 h-3 text-amber-500 shrink-0" />
        <span className="text-xs font-bold text-amber-500 truncate">{d.name}</span>
      </div>

      {d.landing_info && (
        <div className="text-[9px] text-muted-foreground">
          {d.landing_info.file_count} file{d.landing_info.file_count !== 1 ? "s" : ""}
        </div>
      )}

      <Handle type="source" position={Position.Right} className="!bg-amber-500 !w-2 !h-2 !border-0" />
    </div>
  );
}

export const LandingNode = memo(LandingNodeComponent);
