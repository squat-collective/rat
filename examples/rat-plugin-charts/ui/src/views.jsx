// Page-level views for the charts plugin: the three list tabs (Dashboards,
// Charts, Reports), the dashboard grid, and the report reader.

import React from "react";
import { api } from "./api.js";
import { LiveChart } from "./chart.jsx";
import { ChartEditor, ReportEditor, NamePrompt } from "./editors.jsx";
import {
  C,
  Card,
  Button,
  Badge,
  Modal,
  Markdown,
  Loading,
  ErrorText,
  EmptyState,
} from "./components.jsx";

function shortDate(s) {
  try {
    return new Date(s).toLocaleDateString();
  } catch (e) {
    return "";
  }
}

// useResource loads from a *stable* loader function (e.g. api.listCharts).
function useResource(loader) {
  const [state, setState] = React.useState({ loading: true });
  const reload = React.useCallback(() => {
    setState({ loading: true });
    loader()
      .then((d) => setState({ loading: false, data: d || [] }))
      .catch((e) => setState({ loading: false, error: String((e && e.message) || e) }));
  }, [loader]);
  React.useEffect(() => {
    reload();
  }, [reload]);
  return { ...state, reload };
}

const gridStyle = {
  display: "grid",
  gridTemplateColumns: "repeat(auto-fill, minmax(260px, 1fr))",
  gap: "1rem",
};

function SectionHeader(props) {
  return (
    <div
      style={{
        display: "flex",
        justifyContent: "space-between",
        alignItems: "center",
        margin: "0.25rem 0 1rem",
      }}
    >
      <div style={{ fontSize: "0.8rem", color: C.muted }}>{props.subtitle}</div>
      {props.action}
    </div>
  );
}

// ── Dashboards tab ────────────────────────────────────────────────

export function DashboardsTab(props) {
  const res = useResource(api.listDashboards);
  const [creating, setCreating] = React.useState(false);

  function create(name) {
    return api.createDashboard({ title: name }).then((d) => {
      setCreating(false);
      props.onOpen(d.id);
    });
  }

  function remove(id) {
    api.deleteDashboard(id).then(res.reload).catch(res.reload);
  }

  return (
    <div>
      <SectionHeader
        subtitle="Modular grids of live charts."
        action={
          <Button variant="primary" onClick={() => setCreating(true)}>
            + New dashboard
          </Button>
        }
      />
      {res.loading ? <Loading /> : null}
      {res.error ? <ErrorText>{res.error}</ErrorText> : null}
      {res.data && res.data.length === 0 ? (
        <EmptyState icon="▦">No dashboards yet — create one to arrange charts.</EmptyState>
      ) : null}
      {res.data && res.data.length ? (
        <div style={gridStyle}>
          {res.data.map((d) => (
            <Card key={d.id} style={{ display: "flex", flexDirection: "column", gap: "0.5rem" }}>
              <div style={{ fontWeight: 700, fontSize: "0.95rem" }}>{d.title}</div>
              <div style={{ fontSize: "0.74rem", color: C.muted }}>
                {(d.widgets || []).length} chart
                {(d.widgets || []).length === 1 ? "" : "s"} · updated {shortDate(d.updated_at)}
              </div>
              <div style={{ display: "flex", gap: "0.4rem", marginTop: "0.2rem" }}>
                <Button variant="primary" onClick={() => props.onOpen(d.id)}>
                  Open
                </Button>
                <Button variant="danger" onClick={() => remove(d.id)}>
                  Delete
                </Button>
              </div>
            </Card>
          ))}
        </div>
      ) : null}
      {creating ? (
        <NamePrompt
          title="New dashboard"
          label="Dashboard name"
          placeholder="e.g. Sales overview"
          onClose={() => setCreating(false)}
          onSubmit={create}
        />
      ) : null}
    </div>
  );
}

// ── Charts tab ────────────────────────────────────────────────────

export function ChartsTab() {
  const res = useResource(api.listCharts);
  const [editing, setEditing] = React.useState(false);

  function remove(id) {
    api.deleteChart(id).then(res.reload).catch(res.reload);
  }

  return (
    <div>
      <SectionHeader
        subtitle="Each chart re-runs its SQL live."
        action={
          <Button variant="primary" onClick={() => setEditing(true)}>
            + New chart
          </Button>
        }
      />
      {res.loading ? <Loading /> : null}
      {res.error ? <ErrorText>{res.error}</ErrorText> : null}
      {res.data && res.data.length === 0 ? (
        <EmptyState icon="▤">No charts yet — build one from a SQL query.</EmptyState>
      ) : null}
      {res.data && res.data.length ? (
        <div
          style={{
            display: "grid",
            gridTemplateColumns: "repeat(auto-fill, minmax(340px, 1fr))",
            gap: "1rem",
          }}
        >
          {res.data.map((c) => (
            <Card key={c.id} style={{ display: "flex", flexDirection: "column", gap: "0.5rem" }}>
              <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center" }}>
                <div style={{ fontWeight: 700, fontSize: "0.9rem" }}>{c.title}</div>
                <Badge>{c.type}</Badge>
              </div>
              <LiveChart chartId={c.id} height={200} />
              <div style={{ display: "flex", justifyContent: "flex-end" }}>
                <Button variant="danger" onClick={() => remove(c.id)}>
                  Delete
                </Button>
              </div>
            </Card>
          ))}
        </div>
      ) : null}
      {editing ? (
        <ChartEditor
          onClose={() => setEditing(false)}
          onSaved={() => {
            setEditing(false);
            res.reload();
          }}
        />
      ) : null}
    </div>
  );
}

// ── Reports tab ───────────────────────────────────────────────────

export function ReportsTab(props) {
  const res = useResource(api.listReports);
  const [editing, setEditing] = React.useState(false);

  function remove(id) {
    api.deleteReport(id).then(res.reload).catch(res.reload);
  }

  return (
    <div>
      <SectionHeader
        subtitle="Narrative documents interleaving text and charts."
        action={
          <Button variant="primary" onClick={() => setEditing(true)}>
            + New report
          </Button>
        }
      />
      {res.loading ? <Loading /> : null}
      {res.error ? <ErrorText>{res.error}</ErrorText> : null}
      {res.data && res.data.length === 0 ? (
        <EmptyState icon="▥">No reports yet — compose one from text and charts.</EmptyState>
      ) : null}
      {res.data && res.data.length ? (
        <div style={gridStyle}>
          {res.data.map((r) => (
            <Card key={r.id} style={{ display: "flex", flexDirection: "column", gap: "0.5rem" }}>
              <div style={{ fontWeight: 700, fontSize: "0.95rem" }}>{r.title}</div>
              <div style={{ fontSize: "0.74rem", color: C.muted }}>
                {(r.blocks || []).length} block
                {(r.blocks || []).length === 1 ? "" : "s"} · {shortDate(r.created_at)}
              </div>
              <div style={{ display: "flex", gap: "0.4rem", marginTop: "0.2rem" }}>
                <Button variant="primary" onClick={() => props.onOpen(r.id)}>
                  Open
                </Button>
                <Button variant="danger" onClick={() => remove(r.id)}>
                  Delete
                </Button>
              </div>
            </Card>
          ))}
        </div>
      ) : null}
      {editing ? (
        <ReportEditor
          onClose={() => setEditing(false)}
          onSaved={() => {
            setEditing(false);
            res.reload();
          }}
        />
      ) : null}
    </div>
  );
}

// ── Add-chart picker (used inside the dashboard view) ─────────────

function AddChartModal(props) {
  const res = useResource(api.listCharts);
  return (
    <Modal title="Add a chart" onClose={props.onClose}>
      {res.loading ? <Loading /> : null}
      {res.error ? <ErrorText>{res.error}</ErrorText> : null}
      {res.data && res.data.length === 0 ? (
        <EmptyState icon="▤">No charts to add — create one in the Charts tab first.</EmptyState>
      ) : null}
      {res.data && res.data.length ? (
        <div style={{ display: "flex", flexDirection: "column", gap: "0.4rem" }}>
          {res.data.map((c) => (
            <button
              key={c.id}
              onClick={() => props.onPick(c.id)}
              style={{
                display: "flex",
                justifyContent: "space-between",
                alignItems: "center",
                padding: "0.55rem 0.7rem",
                border: "1px solid " + C.border,
                background: C.card,
                color: C.fg,
                cursor: "pointer",
                fontFamily: "inherit",
                fontSize: "0.85rem",
              }}
            >
              <span>{c.title}</span>
              <Badge>{c.type}</Badge>
            </button>
          ))}
        </div>
      ) : null}
    </Modal>
  );
}

// ── Dashboard view ────────────────────────────────────────────────

function widgetHeight(h) {
  return 150 + (Math.max(1, h || 1) - 1) * 130;
}

export function DashboardView(props) {
  const id = props.id;
  const [dash, setDash] = React.useState(null);
  const [err, setErr] = React.useState("");
  const [editing, setEditing] = React.useState(false);
  const [adding, setAdding] = React.useState(false);
  const [refreshKey, setRefreshKey] = React.useState(0);

  React.useEffect(() => {
    let alive = true;
    setErr("");
    api
      .getDashboard(id)
      .then((d) => {
        if (alive) setDash(d);
      })
      .catch((e) => {
        if (alive) setErr(String((e && e.message) || e));
      });
    return () => {
      alive = false;
    };
  }, [id]);

  function persist(widgets) {
    const next = { ...dash, widgets: widgets };
    setDash(next);
    api.updateDashboard(id, { widgets: widgets }).catch((e) => setErr(String((e && e.message) || e)));
  }

  function addWidget(chartId) {
    setAdding(false);
    persist((dash.widgets || []).concat([{ chart_id: chartId, width: 2, height: 1 }]));
  }
  function removeWidget(i) {
    persist((dash.widgets || []).filter((_, j) => j !== i));
  }
  function setWidth(i, w) {
    persist((dash.widgets || []).map((wg, j) => (j === i ? { ...wg, width: w } : wg)));
  }
  function setHeight(i, h) {
    persist((dash.widgets || []).map((wg, j) => (j === i ? { ...wg, height: h } : wg)));
  }
  function moveWidget(i, dir) {
    const ws = (dash.widgets || []).slice();
    const j = i + dir;
    if (j < 0 || j >= ws.length) return;
    const tmp = ws[i];
    ws[i] = ws[j];
    ws[j] = tmp;
    persist(ws);
  }

  if (err) return <ErrorText>{err}</ErrorText>;
  if (!dash) return <Loading text="Loading dashboard…" />;

  const widgets = dash.widgets || [];

  return (
    <div>
      <div
        style={{
          display: "flex",
          justifyContent: "space-between",
          alignItems: "center",
          marginBottom: "1rem",
          flexWrap: "wrap",
          gap: "0.5rem",
        }}
      >
        <div style={{ display: "flex", alignItems: "center", gap: "0.6rem" }}>
          <Button onClick={props.onBack}>← Back</Button>
          <h2 style={{ fontWeight: 700, fontSize: "1.05rem" }}>{dash.title}</h2>
        </div>
        <div style={{ display: "flex", gap: "0.4rem" }}>
          <Button onClick={() => setRefreshKey((k) => k + 1)} title="Re-run every chart query">
            ⟳ Refresh
          </Button>
          {editing ? (
            <Button onClick={() => setAdding(true)}>+ Add chart</Button>
          ) : null}
          <Button variant={editing ? "primary" : "default"} onClick={() => setEditing((e) => !e)}>
            {editing ? "Done" : "Edit"}
          </Button>
        </div>
      </div>

      {widgets.length === 0 ? (
        <EmptyState icon="▦">
          This dashboard is empty.{" "}
          {editing ? "Use “+ Add chart”." : "Hit “Edit”, then add charts."}
        </EmptyState>
      ) : (
        <div style={{ display: "grid", gridTemplateColumns: "repeat(4, 1fr)", gap: "1rem" }}>
          {widgets.map((wg, i) => (
            <Card
              key={i}
              style={{
                gridColumn: "span " + Math.min(4, Math.max(1, wg.width || 2)),
                padding: "0.75rem",
              }}
            >
              {editing ? (
                <div
                  style={{
                    display: "flex",
                    justifyContent: "space-between",
                    alignItems: "center",
                    marginBottom: "0.4rem",
                    gap: "0.4rem",
                    flexWrap: "wrap",
                  }}
                >
                  <div style={{ display: "flex", gap: "0.2rem", alignItems: "center" }}>
                    <span style={{ fontSize: "0.62rem", color: C.muted, marginRight: "0.2rem" }}>
                      W
                    </span>
                    {[1, 2, 3, 4].map((w) => (
                      <Button
                        key={w}
                        variant={wg.width === w ? "primary" : "default"}
                        onClick={() => setWidth(i, w)}
                        style={{ padding: "0.1rem 0.4rem" }}
                      >
                        {w}
                      </Button>
                    ))}
                    <span
                      style={{ fontSize: "0.62rem", color: C.muted, margin: "0 0.2rem 0 0.5rem" }}
                    >
                      H
                    </span>
                    {[1, 2, 3].map((hh) => (
                      <Button
                        key={hh}
                        variant={(wg.height || 1) === hh ? "primary" : "default"}
                        onClick={() => setHeight(i, hh)}
                        style={{ padding: "0.1rem 0.4rem" }}
                      >
                        {hh}
                      </Button>
                    ))}
                  </div>
                  <div style={{ display: "flex", gap: "0.2rem" }}>
                    <Button
                      variant="ghost"
                      onClick={() => moveWidget(i, -1)}
                      disabled={i === 0}
                      style={{ padding: "0.1rem 0.4rem" }}
                    >
                      ←
                    </Button>
                    <Button
                      variant="ghost"
                      onClick={() => moveWidget(i, 1)}
                      disabled={i === widgets.length - 1}
                      style={{ padding: "0.1rem 0.4rem" }}
                    >
                      →
                    </Button>
                    <Button
                      variant="danger"
                      onClick={() => removeWidget(i)}
                      style={{ padding: "0.1rem 0.4rem" }}
                    >
                      ✕
                    </Button>
                  </div>
                </div>
              ) : null}
              <LiveChart chartId={wg.chart_id} height={widgetHeight(wg.height)} refreshKey={refreshKey} />
            </Card>
          ))}
        </div>
      )}

      {adding ? <AddChartModal onClose={() => setAdding(false)} onPick={addWidget} /> : null}
    </div>
  );
}

// ── Report view ───────────────────────────────────────────────────

export function ReportView(props) {
  const id = props.id;
  const [report, setReport] = React.useState(null);
  const [err, setErr] = React.useState("");

  React.useEffect(() => {
    let alive = true;
    setErr("");
    api
      .getReport(id)
      .then((r) => {
        if (alive) setReport(r);
      })
      .catch((e) => {
        if (alive) setErr(String((e && e.message) || e));
      });
    return () => {
      alive = false;
    };
  }, [id]);

  if (err) return <ErrorText>{err}</ErrorText>;
  if (!report) return <Loading text="Loading report…" />;

  return (
    <div>
      <div
        style={{
          display: "flex",
          justifyContent: "space-between",
          alignItems: "center",
          marginBottom: "1rem",
        }}
      >
        <div style={{ display: "flex", alignItems: "center", gap: "0.6rem" }}>
          <Button onClick={props.onBack}>← Back</Button>
          <h2 style={{ fontWeight: 700, fontSize: "1.05rem" }}>{report.title}</h2>
        </div>
        <Button onClick={() => window.print()}>🖨 Print</Button>
      </div>

      <div style={{ maxWidth: "46rem", margin: "0 auto" }}>
        {(report.blocks || []).length === 0 ? (
          <EmptyState icon="▥">This report has no blocks.</EmptyState>
        ) : null}
        {(report.blocks || []).map((b, i) =>
          b.kind === "text" ? (
            <div key={i} style={{ margin: "0.75rem 0", fontSize: "0.9rem", lineHeight: 1.6 }}>
              <Markdown text={b.text} />
            </div>
          ) : (
            <Card key={i} style={{ margin: "1rem 0", padding: "0.85rem" }}>
              <LiveChart chartId={b.chart_id} height={300} />
            </Card>
          ),
        )}
      </div>
    </div>
  );
}
