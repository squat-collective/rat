// rat-plugin-charts — portal UI bundle entry point.
//
// Registers the "Dashboards" plugin with the portal: an /x/charts app with
// Dashboards, Charts and Reports. Bundled by esbuild together with Recharts
// (see build.mjs); React itself comes from the portal via window.React.

import React from "react";
import {
  DashboardsTab,
  ChartsTab,
  ReportsTab,
  DashboardView,
  ReportView,
} from "./views.jsx";
import { C } from "./components.jsx";

// The portal renders a plugin route with path={segments} — the URL parts
// after /x/. We sub-route inside the single /x/charts route from those.
function parsePath(path) {
  const p = path || [];
  if (p[1] === "d" && p[2]) return { kind: "dashboard", id: p[2] };
  if (p[1] === "r" && p[2]) return { kind: "report", id: p[2] };
  return { kind: "home", tab: "dashboards" };
}

function urlFor(view) {
  if (view.kind === "dashboard") return "/x/charts/d/" + view.id;
  if (view.kind === "report") return "/x/charts/r/" + view.id;
  return "/x/charts";
}

function TabButton(props) {
  return (
    <button
      onClick={props.onClick}
      style={{
        padding: "0.45rem 0.9rem",
        fontSize: "0.82rem",
        fontWeight: 700,
        fontFamily: "inherit",
        cursor: "pointer",
        background: "transparent",
        color: props.active ? C.fg : C.muted,
        border: "none",
        borderBottom: "2px solid " + (props.active ? C.primary : "transparent"),
      }}
    >
      {props.label}
    </button>
  );
}

function ChartsApp(props) {
  const [view, setView] = React.useState(() => parsePath(props.path));

  // Navigation is state-based; the URL is kept in sync with replaceState so a
  // dashboard or report is still deep-linkable on a fresh page load.
  function nav(next) {
    setView(next);
    try {
      window.history.replaceState({}, "", urlFor(next));
    } catch (e) {
      /* replaceState can fail in odd embedding contexts — navigation still works */
    }
  }
  const home = (tab) => nav({ kind: "home", tab: tab || "dashboards" });

  let content;
  if (view.kind === "dashboard") {
    content = <DashboardView id={view.id} onBack={() => home("dashboards")} />;
  } else if (view.kind === "report") {
    content = <ReportView id={view.id} onBack={() => home("reports")} />;
  } else {
    const tab = view.tab || "dashboards";
    content = (
      <div>
        <div
          style={{
            display: "flex",
            gap: "0.25rem",
            borderBottom: "1px solid " + C.border,
            marginBottom: "1.25rem",
          }}
        >
          <TabButton label="Dashboards" active={tab === "dashboards"} onClick={() => home("dashboards")} />
          <TabButton label="Charts" active={tab === "charts"} onClick={() => home("charts")} />
          <TabButton label="Reports" active={tab === "reports"} onClick={() => home("reports")} />
        </div>
        {tab === "dashboards" ? (
          <DashboardsTab onOpen={(id) => nav({ kind: "dashboard", id: id })} />
        ) : null}
        {tab === "charts" ? <ChartsTab /> : null}
        {tab === "reports" ? (
          <ReportsTab onOpen={(id) => nav({ kind: "report", id: id })} />
        ) : null}
      </div>
    );
  }

  return (
    <div style={{ maxWidth: "72rem", margin: "0 auto" }}>
      <h1 style={{ fontWeight: 800, fontSize: "1.4rem", marginBottom: "0.15rem" }}>
        📊 Dashboards
      </h1>
      <p style={{ fontSize: "0.8rem", color: C.muted, margin: "0 0 1.1rem" }}>
        Charts, modular dashboards and reports — every chart re-runs its query live.
      </p>
      {content}
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
