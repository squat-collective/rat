// Entry point. Injects React Flow's stylesheet into the document, then
// registers the LineageApp component with the host portal's plugin
// loader. Both React and ReactDOM are sourced from window.React/
// window.ReactDOM via the shims/ aliases — we never pull our own copy.
import flowCss from "@xyflow/react/dist/style.css";
import { LineageApp } from "./LineagePage";

(function () {
  // Inject React Flow CSS once.
  const STYLE_ID = "rat-plugin-lineage-styles";
  if (!document.getElementById(STYLE_ID)) {
    const style = document.createElement("style");
    style.id = STYLE_ID;
    style.textContent = String(flowCss);
    document.head.appendChild(style);
  }

  const w = window as unknown as {
    __RAT_REGISTER_PLUGIN?: (name: string, reg: unknown) => void;
    React?: unknown;
  };
  if (!w.__RAT_REGISTER_PLUGIN) {
    console.warn("[lineage] RAT plugin host not available — skipping");
    return;
  }
  if (!w.React) {
    console.warn("[lineage] window.React missing — the host portal must expose it");
    return;
  }

  w.__RAT_REGISTER_PLUGIN("lineage", {
    navItems: [{ label: "Lineage", icon: "git-branch", href: "/x/lineage", priority: 15 }],
    routes: [{ path: "/x/lineage", component: LineageApp }],
  });
  console.info("[lineage] registered with the portal");
})();
