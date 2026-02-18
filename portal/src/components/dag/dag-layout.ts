import dagre from "dagre";
import type { Node, Edge } from "@xyflow/react";

const NODE_DIMENSIONS: Record<string, { width: number; height: number }> = {
  pipeline: { width: 220, height: 100 },
  table: { width: 200, height: 90 },
  landing_zone: { width: 180, height: 80 },
};

/** Apply dagre auto-layout to React Flow nodes + edges (left-to-right). */
export function layoutGraph(
  nodes: Node[],
  edges: Edge[],
): { nodes: Node[]; edges: Edge[] } {
  const g = new dagre.graphlib.Graph();
  g.setDefaultEdgeLabel(() => ({}));
  g.setGraph({ rankdir: "LR", nodesep: 60, ranksep: 120 });

  for (const node of nodes) {
    const dims = NODE_DIMENSIONS[node.type ?? "pipeline"] ?? NODE_DIMENSIONS.pipeline;
    g.setNode(node.id, { width: dims.width, height: dims.height });
  }

  for (const edge of edges) {
    g.setEdge(edge.source, edge.target);
  }

  dagre.layout(g);

  const layoutedNodes = nodes.map((node) => {
    const pos = g.node(node.id);
    const dims = NODE_DIMENSIONS[node.type ?? "pipeline"] ?? NODE_DIMENSIONS.pipeline;
    return {
      ...node,
      position: {
        x: pos.x - dims.width / 2,
        y: pos.y - dims.height / 2,
      },
    };
  });

  return { nodes: layoutedNodes, edges };
}
