"use client";

import { useEffect, useRef, useState } from "react";

interface MermaidProps {
  chart: string;
}

export function Mermaid({ chart }: MermaidProps) {
  const ref = useRef<HTMLDivElement>(null);
  const [svg, setSvg] = useState<string>("");

  useEffect(() => {
    const renderChart = async () => {
      const { default: mermaid } = await import("mermaid");
      mermaid.initialize({
        startOnLoad: false,
        theme: "dark",
        themeVariables: {
          primaryColor: "#22c55e",
          primaryTextColor: "#e5e5e5",
          primaryBorderColor: "#22c55e",
          lineColor: "#a855f7",
          secondaryColor: "#121212",
          tertiaryColor: "#1a1a1a",
          background: "#0a0a0a",
          mainBkg: "#121212",
          nodeBorder: "#22c55e",
          clusterBkg: "#1a1a1a",
          clusterBorder: "#333",
          titleColor: "#22c55e",
          edgeLabelBackground: "#0a0a0a",
          fontSize: "14px",
        },
        flowchart: { curve: "basis", padding: 15 },
        sequence: { actorMargin: 50, messageMargin: 40 },
      });

      const id = `mermaid-${Math.random().toString(36).slice(2, 9)}`;
      try {
        const { svg: renderedSvg } = await mermaid.render(id, chart.trim());
        setSvg(renderedSvg);
      } catch (e) {
        console.error("Mermaid render error:", e);
        setSvg(`<pre style="color: red;">Mermaid error: ${e}</pre>`);
      }
    };

    renderChart();
  }, [chart]);

  return (
    <div className="mermaid-wrapper">
      <div ref={ref} dangerouslySetInnerHTML={{ __html: svg }} />
    </div>
  );
}
