// Component renderers — one per dashboard component type. ComponentBody
// dispatches on the component's type; chart and metric components fetch their
// data live via the plugin's /query endpoint.

import React from "react";
import { api } from "./api.js";
import { ChartView } from "./chart.jsx";
import { C, Markdown, ErrorText } from "./components.jsx";

// useQuery runs SQL via /query. A changing sql or refreshKey re-runs it.
export function useQuery(sql, refreshKey) {
  const [st, setSt] = React.useState({ loading: true });
  React.useEffect(() => {
    if (!sql || !String(sql).trim()) {
      setSt({ loading: false, rows: [] });
      return;
    }
    let alive = true;
    setSt({ loading: true });
    api
      .query(sql)
      .then((res) => {
        if (alive) setSt({ loading: false, rows: res.rows || [], error: res.error });
      })
      .catch((e) => {
        if (alive) setSt({ loading: false, error: String((e && e.message) || e) });
      });
    return () => {
      alive = false;
    };
  }, [sql, refreshKey]);
  return st;
}

function num(v) {
  if (typeof v === "number") return v;
  const n = Number(v);
  return Number.isFinite(n) ? n : null;
}

function fmtNum(v) {
  if (Number.isInteger(v)) return v.toLocaleString();
  return v.toLocaleString(undefined, { maximumFractionDigits: 2 });
}

function fill(text) {
  return (
    <div
      style={{
        height: "100%",
        display: "flex",
        alignItems: "center",
        justifyContent: "center",
        color: C.muted,
        fontSize: "0.78rem",
      }}
    >
      {text}
    </div>
  );
}

// ── Heading ───────────────────────────────────────────────────────

function HeadingBody(props) {
  const p = props.p;
  const level = p.level || 1;
  const size = { 1: "1.65rem", 2: "1.2rem", 3: "1rem" }[level] || "1.65rem";
  return (
    <div
      style={{
        fontSize: size,
        fontWeight: level === 1 ? 800 : 700,
        display: "flex",
        alignItems: "center",
        height: "100%",
        color: C.fg,
      }}
    >
      {p.text || "Heading"}
    </div>
  );
}

// ── Markdown ──────────────────────────────────────────────────────

function MarkdownBody(props) {
  return (
    <div style={{ height: "100%", overflowY: "auto", fontSize: "0.85rem", lineHeight: 1.55 }}>
      <Markdown text={props.p.markdown || "_Empty text block_"} />
    </div>
  );
}

// ── Metric ────────────────────────────────────────────────────────

function MetricBody(props) {
  const p = props.p;
  const q = useQuery(p.sql, props.refreshKey);

  if (q.loading) return fill("…");
  if (q.error) return <ErrorText>{q.error}</ErrorText>;

  let value = "—";
  if (q.rows && q.rows.length) {
    const row = q.rows[0];
    let v;
    if (p.value_column && row[p.value_column] !== undefined) {
      v = row[p.value_column];
    } else {
      const keys = Object.keys(row);
      const numKey = keys.find((k) => num(row[k]) !== null);
      v = numKey !== undefined ? row[numKey] : row[keys[0]];
    }
    value = num(v) !== null ? fmtNum(num(v)) : String(v);
  }

  return (
    <div
      style={{
        height: "100%",
        display: "flex",
        flexDirection: "column",
        justifyContent: "center",
      }}
    >
      <div
        style={{
          fontSize: "2.4rem",
          fontWeight: 800,
          lineHeight: 1.05,
          color: p.color || C.primary,
          wordBreak: "break-word",
        }}
      >
        {value}
        {p.unit ? <span style={{ fontSize: "1rem", opacity: 0.6 }}> {p.unit}</span> : null}
      </div>
      {p.label ? (
        <div style={{ fontSize: "0.8rem", color: C.muted, marginTop: "0.35rem" }}>{p.label}</div>
      ) : null}
    </div>
  );
}

// ── Chart ─────────────────────────────────────────────────────────

function ChartBody(props) {
  const p = props.p;
  const q = useQuery(p.sql, props.refreshKey);

  if (q.loading) return fill("Loading chart…");
  if (q.error) return <ErrorText>{q.error}</ErrorText>;
  return (
    <ChartView
      height="100%"
      rows={q.rows}
      chart={{
        type: p.chart_type,
        x_column: p.x_column,
        y_columns: p.y_columns,
        options: p.options,
      }}
    />
  );
}

// ── AI analysis ───────────────────────────────────────────────────

// resolveData fetches the rows of the chart/metric component an AI component
// points at, as a compact JSON string for the analysis prompt.
async function resolveData(p, components) {
  if (!p.source) return "";
  const src = (components || []).find((c) => c.id === p.source);
  if (!src || !src.props || !src.props.sql) return "";
  const res = await api.query(src.props.sql);
  if (res.error) return "(query error: " + res.error + ")";
  return JSON.stringify((res.rows || []).slice(0, 50));
}

function AIBody(props) {
  const { component, p, components, onUpdate } = props;
  const [busy, setBusy] = React.useState(false);
  const [err, setErr] = React.useState("");

  function regenerate() {
    setBusy(true);
    setErr("");
    resolveData(p, components)
      .then((data) => api.analyze(p.prompt || "Give a concise analysis of this data.", data))
      .then((res) => {
        onUpdate(component.id, { ...p, analysis: res.analysis || "(no analysis returned)" });
        setBusy(false);
      })
      .catch((e) => {
        setErr(String((e && e.message) || e));
        setBusy(false);
      });
  }

  // Auto-generate once when the component is first shown with no analysis yet.
  React.useEffect(() => {
    if (!p.analysis && p.prompt) regenerate();
    // eslint-disable-next-line
  }, []);

  return (
    <div style={{ height: "100%", display: "flex", flexDirection: "column" }}>
      <div style={{ flex: 1, minHeight: 0, overflowY: "auto", fontSize: "0.84rem", lineHeight: 1.55 }}>
        {err ? (
          <ErrorText>{err}</ErrorText>
        ) : busy ? (
          <div style={{ color: C.muted, fontSize: "0.8rem" }}>✨ Analysing…</div>
        ) : p.analysis ? (
          <Markdown text={p.analysis} />
        ) : (
          <div style={{ color: C.muted, fontSize: "0.8rem" }}>
            No analysis yet — hit Regenerate.
          </div>
        )}
      </div>
      <div style={{ marginTop: "0.4rem", textAlign: "right" }}>
        <button
          onClick={regenerate}
          disabled={busy}
          style={{
            fontSize: "0.68rem",
            fontWeight: 600,
            padding: "0.2rem 0.5rem",
            border: "1px solid " + C.border,
            background: "transparent",
            color: C.fg,
            cursor: busy ? "default" : "pointer",
            opacity: busy ? 0.5 : 1,
            fontFamily: "inherit",
          }}
        >
          ↻ Regenerate
        </button>
      </div>
    </div>
  );
}

// ── Dispatch ──────────────────────────────────────────────────────

// ComponentBody renders one component's content. components (the whole grid)
// is passed so an AI component can resolve the chart it analyses; onUpdate
// persists a component's props (the AI component caches its analysis).
export function ComponentBody(props) {
  const c = props.component;
  const p = c.props || {};
  switch (c.type) {
    case "heading":
      return <HeadingBody p={p} />;
    case "markdown":
      return <MarkdownBody p={p} />;
    case "metric":
      return <MetricBody p={p} refreshKey={props.refreshKey} />;
    case "chart":
      return <ChartBody p={p} refreshKey={props.refreshKey} />;
    case "ai":
      return (
        <AIBody
          component={c}
          p={p}
          components={props.components}
          onUpdate={props.onUpdateComponent}
          refreshKey={props.refreshKey}
        />
      );
    default:
      return <ErrorText>unknown component type: {c.type}</ErrorText>;
  }
}
