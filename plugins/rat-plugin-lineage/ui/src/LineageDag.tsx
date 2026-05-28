// The React Flow canvas. Same node types, same edge styling, same
// click-through navigation as the old portal page — just with a self-
// contained styling palette and no @squat-collective/rat-client type
// import.

import { useCallback, useEffect, useMemo } from "react";
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
import { layoutGraph } from "./dag-layout";
import { PipelineNode, TableNode, LandingNode } from "./nodes";
import type { LineageGraph } from "./types";

const nodeTypes = {
  pipeline: PipelineNode,
  table: TableNode,
  landing_zone: LandingNode,
};

const EDGE_STYLES: Record<string, { stroke: string; animated?: boolean }> = {
  ref: { stroke: "hsl(142, 71%, 45%)" },
  produces: { stroke: "hsl(186, 100%, 50%)", animated: true },
  landing_input: { stroke: "hsl(38, 92%, 50%)" },
};

interface Props {
  graph: LineageGraph;
}

export function LineageDag({ graph }: Props) {
  const { nodes: initialNodes, edges: initialEdges } = useMemo(() => {
    const rawNodes: Node[] = graph.nodes.map((n) => ({
      id: n.id,
      type: n.type,
      position: { x: 0, y: 0 },
      data: { ...n } as unknown as Record<string, unknown>,
    }));

    const rawEdges: Edge[] = graph.edges.map((e, i) => {
      const style = EDGE_STYLES[e.type] ?? EDGE_STYLES.ref;
      return {
        id: "edge-" + i,
        source: e.source,
        target: e.target,
        style: { stroke: style.stroke, strokeWidth: 2 },
        animated: style.animated ?? false,
      };
    });

    return layoutGraph(rawNodes, rawEdges);
  }, [graph]);

  // useNodesState / useEdgesState only set their state from the
  // initial value on first render — when `graph` changes (e.g. the
  // user picks a different namespace), the props update but the
  // internal state would otherwise stay stale. Sync via setNodes /
  // setEdges whenever the recomputed initial values change. This is
  // the same bug that affected the old portal page; fixed once here
  // in the plugin.
  const [nodes, setNodes, onNodesChange] = useNodesState(initialNodes);
  const [edges, setEdges, onEdgesChange] = useEdgesState(initialEdges);
  useEffect(() => {
    setNodes(initialNodes);
    setEdges(initialEdges);
  }, [initialNodes, initialEdges, setNodes, setEdges]);

  // Click-through navigation. We use a plain history pushState +
  // popstate event so we don't depend on next/navigation (which we
  // can't import from a build-free plugin). The portal listens for
  // history changes and re-renders the matching route.
  const onNodeClick: NodeMouseHandler = useCallback((_, node) => {
    const d = node.data as { type?: string; namespace?: string; layer?: string; name?: string };
    const ns = d.namespace ?? "";
    const layer = d.layer ?? "";
    const name = d.name ?? "";
    let path: string | null = null;
    switch (d.type) {
      case "pipeline":
        path = `/pipelines/${ns}/${layer}/${name}`;
        break;
      case "table":
        path = `/explorer/${ns}/${layer}/${name}`;
        break;
      case "landing_zone":
        path = `/landing/${ns}/${name}`;
        break;
    }
    if (path) {
      window.history.pushState({}, "", path);
      window.dispatchEvent(new PopStateEvent("popstate"));
    }
  }, []);

  return (
    <div
      style={{
        width: "100%",
        height: "calc(100vh - 10rem)",
        border: "2px solid hsl(var(--border, 0 0% 16%))",
      }}
    >
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
          style={{ background: "hsl(0 0% 7%)", border: "1px solid hsl(0 0% 16%)" }}
          nodeColor={(n) => {
            switch (n.type) {
              case "pipeline":
                return "hsl(142, 71%, 45%)";
              case "table":
                return "hsl(186, 100%, 50%)";
              case "landing_zone":
                return "hsl(38, 92%, 50%)";
              default:
                return "#666";
            }
          }}
        />
        <Controls />
      </ReactFlow>
    </div>
  );
}
