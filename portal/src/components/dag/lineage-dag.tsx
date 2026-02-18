"use client";

import { useCallback, useMemo } from "react";
import {
  ReactFlow,
  Background,
  Controls,
  MiniMap,
  useNodesState,
  useEdgesState,
  type Node,
  type Edge,
  type NodeMouseHandler,
} from "@xyflow/react";
import "@xyflow/react/dist/style.css";
import { useRouter } from "next/navigation";
import type { LineageGraph } from "@squat-collective/rat-client";
import { layoutGraph } from "./dag-layout";
import { PipelineNode } from "./pipeline-node";
import { TableNode } from "./table-node";
import { LandingNode } from "./landing-node";

const nodeTypes = {
  pipeline: PipelineNode,
  table: TableNode,
  landing_zone: LandingNode,
};

const EDGE_STYLES: Record<string, { stroke: string; animated?: boolean }> = {
  ref: { stroke: "hsl(142, 71%, 45%)" },            // green
  produces: { stroke: "hsl(186, 100%, 50%)", animated: true }, // cyan, animated
  landing_input: { stroke: "hsl(38, 92%, 50%)" },   // amber
};

interface LineageDagProps {
  graph: LineageGraph;
}

export function LineageDag({ graph }: LineageDagProps) {
  const router = useRouter();

  const { nodes: initialNodes, edges: initialEdges } = useMemo(() => {
    const rawNodes: Node[] = graph.nodes.map((n) => ({
      id: n.id,
      type: n.type,
      position: { x: 0, y: 0 },
      data: { ...n },
    }));

    const rawEdges: Edge[] = graph.edges.map((e, i) => {
      const style = EDGE_STYLES[e.type] ?? EDGE_STYLES.ref;
      return {
        id: `edge-${i}`,
        source: e.source,
        target: e.target,
        style: { stroke: style.stroke, strokeWidth: 2 },
        animated: style.animated ?? false,
      };
    });

    return layoutGraph(rawNodes, rawEdges);
  }, [graph]);

  const [nodes, , onNodesChange] = useNodesState(initialNodes);
  const [edges, , onEdgesChange] = useEdgesState(initialEdges);

  const onNodeClick: NodeMouseHandler = useCallback(
    (_, node) => {
      const d = node.data as Record<string, unknown>;
      const ns = d.namespace as string;
      const layer = d.layer as string;
      const name = d.name as string;
      const type = d.type as string;

      switch (type) {
        case "pipeline":
          router.push(`/pipelines/${ns}/${layer}/${name}`);
          break;
        case "table":
          router.push(`/explorer/${ns}/${layer}/${name}`);
          break;
        case "landing_zone":
          router.push(`/landing/${ns}/${name}`);
          break;
      }
    },
    [router],
  );

  return (
    <div className="w-full h-[calc(100vh-10rem)] border-2 border-border">
      <ReactFlow
        nodes={nodes}
        edges={edges}
        onNodesChange={onNodesChange}
        onEdgesChange={onEdgesChange}
        onNodeClick={onNodeClick}
        nodeTypes={nodeTypes}
        fitView
        minZoom={0.1}
        maxZoom={2}
        proOptions={{ hideAttribution: true }}
      >
        <Background color="hsl(142, 71%, 45%)" gap={20} size={1} />
        <MiniMap
          className="!bg-zinc-900 !border-border"
          nodeColor={(n) => {
            switch (n.type) {
              case "pipeline": return "hsl(142, 71%, 45%)";
              case "table": return "hsl(186, 100%, 50%)";
              case "landing_zone": return "hsl(38, 92%, 50%)";
              default: return "#666";
            }
          }}
        />
        <Controls className="!bg-zinc-900 !border-border [&>button]:!bg-zinc-800 [&>button]:!border-border [&>button]:!text-foreground" />
      </ReactFlow>
    </div>
  );
}
