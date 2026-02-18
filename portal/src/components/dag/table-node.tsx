"use client";

import { memo } from "react";
import { Handle, Position } from "@xyflow/react";
import type { NodeProps } from "@xyflow/react";
import { Database } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { LAYER_BADGE_COLORS } from "@/lib/constants";
import { formatBytes } from "@/lib/utils";
import type { LineageNode } from "@squat-collective/rat-client";

type TableNodeData = LineageNode & { label?: string };

function TableNodeComponent({ data }: NodeProps) {
  const d = data as unknown as TableNodeData;
  const layerColor = LAYER_BADGE_COLORS[d.layer ?? ""] ?? "";

  return (
    <div className="brutal-card border-2 border-border bg-zinc-900/50 px-3 py-2 min-w-[180px] cursor-pointer hover:border-neon-cyan/50 transition-colors">
      <Handle type="target" position={Position.Left} className="!bg-neon-cyan !w-2 !h-2 !border-0" />

      <div className="flex items-center gap-2 mb-1">
        <Database className="w-3 h-3 text-neon-cyan shrink-0" />
        <span className="text-xs font-bold text-neon-cyan truncate">{d.name}</span>
        {d.layer && (
          <Badge className={`text-[8px] px-1 py-0 ${layerColor}`}>
            {d.layer}
          </Badge>
        )}
      </div>

      {d.table_stats && (
        <div className="text-[9px] text-muted-foreground space-x-2">
          <span>{d.table_stats.row_count.toLocaleString()} rows</span>
          <span>{formatBytes(d.table_stats.size_bytes)}</span>
        </div>
      )}

      <Handle type="source" position={Position.Right} className="!bg-neon-cyan !w-2 !h-2 !border-0" />
    </div>
  );
}

export const TableNode = memo(TableNodeComponent);
