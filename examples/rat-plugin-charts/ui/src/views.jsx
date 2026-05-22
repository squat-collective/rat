// Page-level views: the dashboard list and the dashboard itself (the editable
// react-grid-layout grid of components).

import React from "react";
import { api } from "./api.js";
import { DashboardGrid } from "./grid.jsx";
import { ComponentEditor, NamePrompt } from "./editors.jsx";
import { C, Card, Button, Modal, TextInput, Loading, ErrorText, EmptyState } from "./components.jsx";

function shortDate(s) {
  try {
    return new Date(s).toLocaleDateString();
  } catch (e) {
    return "";
  }
}

// defaultProps seeds a freshly-added component of each type.
function defaultProps(type) {
  switch (type) {
    case "heading":
      return { text: "Section heading", level: 1 };
    case "markdown":
      return { markdown: "## Notes\n\nWrite here." };
    case "metric":
      return { label: "Metric", color: "#4ade80" };
    case "ai":
      return { prompt: "Summarise the key insights from this data." };
    default:
      return { chart_type: "bar", options: { palette: "rat" } };
  }
}

const ADD_TYPES = [
  { type: "chart", label: "Chart", icon: "▦", desc: "A graph from a SQL query" },
  { type: "metric", label: "Metric", icon: "◷", desc: "A single big KPI number" },
  { type: "ai", label: "AI analysis", icon: "✨", desc: "An AI-written insight about a chart" },
  { type: "heading", label: "Heading", icon: "H", desc: "A section title" },
  { type: "markdown", label: "Text", icon: "¶", desc: "A markdown block" },
];

function AddComponentMenu(props) {
  return (
    <Modal title="Add a component" onClose={props.onClose}>
      <div style={{ display: "flex", flexDirection: "column", gap: "0.4rem" }}>
        {ADD_TYPES.map((t) => (
          <button
            key={t.type}
            onClick={() => props.onPick(t.type)}
            style={{
              display: "flex",
              alignItems: "center",
              gap: "0.75rem",
              padding: "0.6rem 0.7rem",
              border: "1px solid " + C.border,
              background: C.card,
              color: C.fg,
              cursor: "pointer",
              fontFamily: "inherit",
              textAlign: "left",
            }}
          >
            <span style={{ fontSize: "1.1rem", width: "1.6rem", textAlign: "center" }}>
              {t.icon}
            </span>
            <span>
              <div style={{ fontWeight: 700, fontSize: "0.85rem" }}>{t.label}</div>
              <div style={{ fontSize: "0.72rem", color: C.muted }}>{t.desc}</div>
            </span>
          </button>
        ))}
      </div>
    </Modal>
  );
}

// ── Dashboard list ────────────────────────────────────────────────

export function DashboardList(props) {
  const [st, setSt] = React.useState({ loading: true });
  const [creating, setCreating] = React.useState(false);

  const reload = React.useCallback(() => {
    setSt({ loading: true });
    api
      .listDashboards()
      .then((d) => setSt({ loading: false, data: d || [] }))
      .catch((e) => setSt({ loading: false, error: String((e && e.message) || e) }));
  }, []);
  React.useEffect(() => {
    reload();
  }, [reload]);

  function create(name) {
    return api.createDashboard(name).then((d) => {
      setCreating(false);
      props.onOpen(d.id);
    });
  }
  function remove(id) {
    api.deleteDashboard(id).then(reload).catch(reload);
  }

  return (
    <div>
      <div
        style={{
          display: "flex",
          justifyContent: "space-between",
          alignItems: "center",
          margin: "0.25rem 0 1rem",
        }}
      >
        <div style={{ fontSize: "0.8rem", color: C.muted }}>
          Living dashboards — a grid of charts, metrics, text and AI insights.
        </div>
        <Button variant="primary" onClick={() => setCreating(true)}>
          + New dashboard
        </Button>
      </div>

      {st.loading ? <Loading /> : null}
      {st.error ? <ErrorText>{st.error}</ErrorText> : null}
      {st.data && st.data.length === 0 ? (
        <EmptyState icon="▦">No dashboards yet — create your first one.</EmptyState>
      ) : null}
      {st.data && st.data.length ? (
        <div
          style={{
            display: "grid",
            gridTemplateColumns: "repeat(auto-fill, minmax(260px, 1fr))",
            gap: "1rem",
          }}
        >
          {st.data.map((d) => (
            <Card key={d.id} style={{ display: "flex", flexDirection: "column", gap: "0.5rem" }}>
              <div style={{ fontWeight: 700, fontSize: "0.95rem" }}>{d.title}</div>
              <div style={{ fontSize: "0.74rem", color: C.muted }}>
                {(d.components || []).length} component
                {(d.components || []).length === 1 ? "" : "s"} · updated {shortDate(d.updated_at)}
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

// ── Dashboard view ────────────────────────────────────────────────

export function DashboardView(props) {
  const id = props.id;
  const [dash, setDash] = React.useState(null);
  const [err, setErr] = React.useState("");
  const [editing, setEditing] = React.useState(false);
  const [refreshKey, setRefreshKey] = React.useState(0);
  const [adding, setAdding] = React.useState(false);
  const [editorFor, setEditorFor] = React.useState(null); // { component, isNew }

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

  // persist applies a partial patch ({title?|components?}) optimistically and
  // saves it to the server.
  function persist(patch) {
    setDash((cur) => ({ ...cur, ...patch }));
    api.updateDashboard(id, patch).catch((e) => setErr(String((e && e.message) || e)));
  }

  function onLayoutChange(rgl) {
    const byId = {};
    rgl.forEach((l) => {
      byId[l.i] = l;
    });
    const next = dash.components.map((c) => {
      const l = byId[c.id];
      return l ? { ...c, layout: { x: l.x, y: l.y, w: l.w, h: l.h } } : c;
    });
    persist({ components: next });
  }

  function saveComponent(propsObj) {
    const ef = editorFor;
    setEditorFor(null);
    if (!ef) return;
    if (ef.isNew) {
      api
        .addComponent(id, { type: ef.component.type, props: propsObj })
        .then((d) => setDash(d))
        .catch((e) => setErr(String((e && e.message) || e)));
    } else {
      persist({
        components: dash.components.map((c) =>
          c.id === ef.component.id ? { ...c, props: propsObj } : c,
        ),
      });
    }
  }

  function deleteComponent(cid) {
    persist({ components: dash.components.filter((c) => c.id !== cid) });
  }

  // updateComponentProps is used by a component to persist its own state — the
  // AI-analysis component caches its generated text this way.
  function updateComponentProps(cid, propsObj) {
    persist({
      components: dash.components.map((c) => (c.id === cid ? { ...c, props: propsObj } : c)),
    });
  }

  function addType(type) {
    setAdding(false);
    setEditorFor({ component: { type: type, props: defaultProps(type) }, isNew: true });
  }

  if (err) return <ErrorText>{err}</ErrorText>;
  if (!dash) return <Loading text="Loading dashboard…" />;

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
        <div style={{ display: "flex", alignItems: "center", gap: "0.6rem", flex: 1, minWidth: 0 }}>
          <Button onClick={props.onBack}>← Back</Button>
          {editing ? (
            <TextInput
              value={dash.title}
              onChange={(t) => setDash((cur) => ({ ...cur, title: t }))}
              style={{ maxWidth: "20rem", fontWeight: 700 }}
            />
          ) : (
            <h2 style={{ fontWeight: 700, fontSize: "1.05rem" }}>{dash.title}</h2>
          )}
        </div>
        <div style={{ display: "flex", gap: "0.4rem" }}>
          <Button onClick={() => setRefreshKey((k) => k + 1)} title="Re-run every query">
            ⟳ Refresh
          </Button>
          {editing ? <Button onClick={() => setAdding(true)}>+ Add</Button> : null}
          <Button
            variant={editing ? "primary" : "default"}
            onClick={() => {
              if (editing) persist({ title: dash.title }); // save any title edit
              setEditing((e) => !e);
            }}
          >
            {editing ? "Done" : "Edit"}
          </Button>
        </div>
      </div>

      {dash.components.length === 0 ? (
        <EmptyState icon="▦">
          This dashboard is empty.{" "}
          {editing ? "Use “+ Add” to place components." : "Hit “Edit”, then add components."}
        </EmptyState>
      ) : (
        <DashboardGrid
          components={dash.components}
          editing={editing}
          refreshKey={refreshKey}
          onLayoutChange={onLayoutChange}
          onEdit={(c) => setEditorFor({ component: c, isNew: false })}
          onDelete={deleteComponent}
          onUpdateComponent={updateComponentProps}
        />
      )}

      {adding ? <AddComponentMenu onPick={addType} onClose={() => setAdding(false)} /> : null}
      {editorFor ? (
        <ComponentEditor
          component={editorFor.component}
          components={dash.components}
          isNew={editorFor.isNew}
          onClose={() => setEditorFor(null)}
          onSave={saveComponent}
        />
      ) : null}
    </div>
  );
}
