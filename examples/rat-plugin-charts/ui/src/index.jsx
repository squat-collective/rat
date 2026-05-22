// rat-plugin-charts — portal UI bundle entry point.
//
// Registers the "Dashboards" plugin: an /x/charts app with a list of
// dashboards and an editable drag-and-drop grid of components. Bundled by
// esbuild together with Recharts and react-grid-layout (see build.mjs).

import React from "react";
import { ChartView } from "./chart.jsx";
import { DashboardList, DashboardView } from "./views.jsx";
import { C } from "./components.jsx";

// Publish the chart renderer so other plugins can draw charts with the same
// Recharts engine. The AI plugin's chat uses window.__RAT_CHARTS.ChartView to
// render the graphs it generates — both plugins share the portal's React, so
// the component is interchangeable.
if (typeof window !== "undefined") {
  window.__RAT_CHARTS = { ChartView: ChartView };
}

// The portal renders a plugin route with path={segments} — the URL parts after
// /x/. We sub-route inside the single /x/charts route from those.
function parsePath(path) {
  const p = path || [];
  if (p[1] === "d" && p[2]) return { kind: "dashboard", id: p[2] };
  return { kind: "list" };
}

function urlFor(view) {
  return view.kind === "dashboard" ? "/x/charts/d/" + view.id : "/x/charts";
}

function ChartsApp(props) {
  const [view, setView] = React.useState(() => parsePath(props.path));

  function nav(next) {
    setView(next);
    try {
      window.history.replaceState({}, "", urlFor(next));
    } catch (e) {
      /* replaceState can fail in odd embeddings — navigation still works */
    }
  }

  return (
    <div style={{ maxWidth: "80rem", margin: "0 auto" }}>
      <h1 style={{ fontWeight: 800, fontSize: "1.4rem", marginBottom: "0.15rem" }}>
        📊 Dashboards
      </h1>
      <p style={{ fontSize: "0.8rem", color: C.muted, margin: "0 0 1.1rem" }}>
        Living dashboards — drag-and-drop grids of charts, metrics, text and AI insights.
      </p>
      {view.kind === "dashboard" ? (
        <DashboardView id={view.id} onBack={() => nav({ kind: "list" })} />
      ) : (
        <DashboardList onOpen={(id) => nav({ kind: "dashboard", id: id })} />
      )}
    </div>
  );
}

if (window.React && typeof window.__RAT_REGISTER_PLUGIN === "function") {
  window.__RAT_REGISTER_PLUGIN("charts", {
    navItems: [
      { label: "Dashboards", icon: "layout-dashboard", href: "/x/charts", priority: 20 },
    ],
    routes: [{ path: "/x/charts", component: ChartsApp }],
  });
  console.info("[charts] dashboards plugin registered with the portal");
} else {
  console.warn("[charts] RAT plugin host not available — skipping");
}
