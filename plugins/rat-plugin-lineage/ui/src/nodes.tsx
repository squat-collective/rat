// Three custom React Flow node renderers — pipeline, table, landing
// zone. Each shows the essentials for its kind (latest run status,
// row count, file count, etc) and links the connection handles.
//
// Styling is inline so the plugin doesn't depend on the portal's
// shadcn theme / Tailwind classes. Colour palette uses CSS vars from
// the host theme (--foreground, --border, etc) with safe fallbacks.

import { memo } from "react";
import { Handle, Position, type NodeProps } from "@xyflow/react";
import type { LineageNode } from "./types";

const C = {
  border: "hsl(var(--border, 0 0% 16%))",
  bg: "hsl(var(--card, 0 0% 7%))",
  bgMuted: "hsla(0, 0%, 0%, 0.3)",
  muted: "hsl(var(--muted-foreground, 0 0% 50%))",
  fg: "hsl(var(--foreground, 0 0% 90%))",
  primary: "hsl(142, 71%, 45%)", // green = pipeline
  cyan: "hsl(186, 100%, 50%)", // cyan = table
  amber: "hsl(38, 92%, 50%)", // amber = landing zone
  red: "hsl(0, 62%, 50%)",
};

const LAYER_COLORS: Record<string, string> = {
  bronze: "rgba(180, 100, 40, 0.4)",
  silver: "rgba(180, 180, 180, 0.4)",
  gold: "rgba(220, 180, 50, 0.4)",
};

const STATUS_COLORS: Record<string, string> = {
  success: "rgba(34, 197, 94, 0.5)",
  failed: "rgba(239, 68, 68, 0.5)",
  running: "rgba(186, 230, 253, 0.5)",
  pending: "rgba(148, 163, 184, 0.5)",
};

const STATUS_EMOJI: Record<string, string> = {
  success: "✓",
  failed: "✗",
  running: "●",
  pending: "·",
};

function Badge({ text, color }: { text: string; color?: string }) {
  return (
    <span
      style={{
        padding: "1px 5px",
        background: color ?? "rgba(255,255,255,0.08)",
        color: C.fg,
        fontSize: 8,
        textTransform: "uppercase",
        letterSpacing: 0.5,
        border: "1px solid " + C.border,
      }}
    >
      {text}
    </span>
  );
}

function formatBytes(n: number): string {
  if (n < 1024) return n + " B";
  if (n < 1024 * 1024) return (n / 1024).toFixed(1) + " KB";
  if (n < 1024 * 1024 * 1024) return (n / (1024 * 1024)).toFixed(1) + " MB";
  return (n / (1024 * 1024 * 1024)).toFixed(1) + " GB";
}

function PipelineNodeRaw({ data }: NodeProps) {
  const d = data as unknown as LineageNode;
  const runStatus = d.latest_run?.status ?? "";

  return (
    <div
      style={{
        border: "2px solid " + C.border,
        background: C.bg,
        padding: "6px 10px",
        minWidth: 200,
        cursor: "pointer",
        color: C.fg,
        fontSize: 11,
      }}
    >
      <Handle
        type="target"
        position={Position.Left}
        style={{ background: C.primary, width: 8, height: 8, border: 0 }}
      />
      <div style={{ display: "flex", alignItems: "center", gap: 6, marginBottom: 4 }}>
        <span style={{ fontWeight: 700, color: C.primary, overflow: "hidden", textOverflow: "ellipsis" }}>
          {d.name}
        </span>
        {d.layer && <Badge text={d.layer} color={LAYER_COLORS[d.layer]} />}
      </div>
      {d.latest_run && (
        <div style={{ display: "flex", alignItems: "center", gap: 4, marginBottom: 2 }}>
          <span style={{ fontSize: 10 }}>{STATUS_EMOJI[runStatus] ?? ""}</span>
          <Badge text={runStatus} color={STATUS_COLORS[runStatus]} />
        </div>
      )}
      {d.quality && d.quality.total > 0 && (
        <div style={{ fontSize: 9, color: C.muted }}>
          Tests: {d.quality.total}
        </div>
      )}
      <Handle
        type="source"
        position={Position.Right}
        style={{ background: C.primary, width: 8, height: 8, border: 0 }}
      />
    </div>
  );
}

function TableNodeRaw({ data }: NodeProps) {
  const d = data as unknown as LineageNode;
  return (
    <div
      style={{
        border: "2px solid " + C.border,
        background: C.bgMuted,
        padding: "6px 10px",
        minWidth: 180,
        cursor: "pointer",
        color: C.fg,
        fontSize: 11,
      }}
    >
      <Handle
        type="target"
        position={Position.Left}
        style={{ background: C.cyan, width: 8, height: 8, border: 0 }}
      />
      <div style={{ display: "flex", alignItems: "center", gap: 6, marginBottom: 4 }}>
        <span style={{ fontSize: 11, color: C.cyan }}>▦</span>
        <span style={{ fontWeight: 700, color: C.cyan, overflow: "hidden", textOverflow: "ellipsis" }}>
          {d.name}
        </span>
        {d.layer && <Badge text={d.layer} color={LAYER_COLORS[d.layer]} />}
      </div>
      {d.table_stats && (
        <div style={{ fontSize: 9, color: C.muted, display: "flex", gap: 8 }}>
          <span>{d.table_stats.row_count.toLocaleString()} rows</span>
          <span>{formatBytes(d.table_stats.size_bytes)}</span>
        </div>
      )}
      <Handle
        type="source"
        position={Position.Right}
        style={{ background: C.cyan, width: 8, height: 8, border: 0 }}
      />
    </div>
  );
}

function LandingNodeRaw({ data }: NodeProps) {
  const d = data as unknown as LineageNode;
  return (
    <div
      style={{
        border: "2px solid rgba(180, 100, 30, 0.6)",
        background: "rgba(180, 100, 30, 0.08)",
        padding: "6px 10px",
        minWidth: 160,
        cursor: "pointer",
        color: C.fg,
        fontSize: 11,
      }}
    >
      <div style={{ display: "flex", alignItems: "center", gap: 6, marginBottom: 4 }}>
        <span style={{ fontSize: 11, color: C.amber }}>↘</span>
        <span style={{ fontWeight: 700, color: C.amber, overflow: "hidden", textOverflow: "ellipsis" }}>
          {d.name}
        </span>
      </div>
      {d.landing_info && (
        <div style={{ fontSize: 9, color: C.muted }}>
          {d.landing_info.file_count} file{d.landing_info.file_count !== 1 ? "s" : ""}
        </div>
      )}
      <Handle
        type="source"
        position={Position.Right}
        style={{ background: C.amber, width: 8, height: 8, border: 0 }}
      />
    </div>
  );
}

export const PipelineNode = memo(PipelineNodeRaw);
export const TableNode = memo(TableNodeRaw);
export const LandingNode = memo(LandingNodeRaw);
