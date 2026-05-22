// Modal editors: build a chart (with a live preview), compose a report, and a
// small reusable name prompt for creating dashboards.

import React from "react";
import { api } from "./api.js";
import { ChartView } from "./chart.jsx";
import {
  C,
  Modal,
  Field,
  TextInput,
  TextArea,
  Select,
  Button,
  Badge,
  ErrorText,
} from "./components.jsx";

const labelStyle = {
  fontSize: "0.66rem",
  fontWeight: 700,
  letterSpacing: "0.06em",
  textTransform: "uppercase",
  color: C.muted,
  marginBottom: "0.25rem",
};

// ── Chart editor ──────────────────────────────────────────────────

export function ChartEditor(props) {
  const [title, setTitle] = React.useState("");
  const [type, setType] = React.useState("bar");
  const [sql, setSql] = React.useState("");
  const [preview, setPreview] = React.useState(null); // { rows } | { error }
  const [running, setRunning] = React.useState(false);
  const [x, setX] = React.useState("");
  const [ys, setYs] = React.useState([]);
  const [saving, setSaving] = React.useState(false);
  const [saveErr, setSaveErr] = React.useState("");

  const cols =
    preview && preview.rows && preview.rows.length ? Object.keys(preview.rows[0]) : [];

  function run() {
    if (!sql.trim() || running) return;
    setRunning(true);
    setPreview(null);
    api
      .preview(sql)
      .then((res) => {
        if (res.error) {
          setPreview({ error: res.error });
          return;
        }
        const rows = res.rows || [];
        setPreview({ rows: rows });
        if (rows.length) {
          const c = Object.keys(rows[0]);
          setX((prev) => (prev && c.indexOf(prev) !== -1 ? prev : c[0]));
          setYs((prev) => {
            const keep = prev.filter((y) => c.indexOf(y) !== -1);
            if (keep.length) return keep;
            const numeric = c.filter(
              (k) => k !== c[0] && typeof rows[0][k] === "number",
            );
            if (numeric.length) return [numeric[0]];
            return c.length > 1 ? [c[1]] : [];
          });
        }
      })
      .catch((e) => setPreview({ error: String((e && e.message) || e) }))
      .then(() => setRunning(false));
  }

  function toggleY(col) {
    setYs((prev) =>
      prev.indexOf(col) !== -1 ? prev.filter((y) => y !== col) : prev.concat([col]),
    );
  }

  function save() {
    setSaveErr("");
    if (!title.trim()) {
      setSaveErr("A title is required.");
      return;
    }
    if (!x || !ys.length) {
      setSaveErr("Run the query, then pick an X column and at least one Y value.");
      return;
    }
    setSaving(true);
    api
      .createChart({
        title: title.trim(),
        type: type,
        sql: sql.trim(),
        x_column: x,
        y_columns: ys,
      })
      .then((c) => props.onSaved(c))
      .catch((e) => {
        setSaveErr(String((e && e.message) || e));
        setSaving(false);
      });
  }

  return (
    <Modal title="New chart" wide onClose={props.onClose}>
      <Field label="Title">
        <TextInput value={title} onChange={setTitle} placeholder="e.g. Orders by customer" />
      </Field>

      <Field label="Chart type">
        <Select value={type} onChange={setType} style={{ maxWidth: "12rem" }}>
          <option value="bar">Bar</option>
          <option value="line">Line</option>
          <option value="area">Area</option>
          <option value="pie">Pie</option>
        </Select>
      </Field>

      <Field
        label="SQL query"
        hint="A read-only SELECT. Reference tables as namespace.layer.name."
      >
        <TextArea
          value={sql}
          onChange={setSql}
          rows={5}
          placeholder={
            "SELECT name, sum(amount) AS total\nFROM default.bronze.orders\nGROUP BY name"
          }
        />
      </Field>

      <div style={{ marginBottom: "0.75rem" }}>
        <Button onClick={run} disabled={running || !sql.trim()}>
          {running ? "Running…" : "▶ Run query"}
        </Button>
      </div>

      {preview && preview.error ? <ErrorText>{preview.error}</ErrorText> : null}

      {preview && preview.rows ? (
        preview.rows.length === 0 ? (
          <ErrorText>The query ran but returned no rows.</ErrorText>
        ) : (
          <div>
            <div style={{ display: "flex", gap: "1.25rem", flexWrap: "wrap" }}>
              <div style={{ minWidth: "9rem" }}>
                <div style={labelStyle}>X axis (category)</div>
                <Select value={x} onChange={setX}>
                  {cols.map((c) => (
                    <option key={c} value={c}>
                      {c}
                    </option>
                  ))}
                </Select>
              </div>
              <div style={{ flex: 1, minWidth: "12rem" }}>
                <div style={labelStyle}>Y values (series)</div>
                <div style={{ display: "flex", flexWrap: "wrap", gap: "0.3rem 0.8rem" }}>
                  {cols
                    .filter((c) => c !== x)
                    .map((c) => (
                      <label
                        key={c}
                        style={{
                          fontSize: "0.78rem",
                          display: "flex",
                          alignItems: "center",
                          gap: "0.3rem",
                        }}
                      >
                        <input
                          type="checkbox"
                          checked={ys.indexOf(c) !== -1}
                          onChange={() => toggleY(c)}
                        />
                        {c}
                      </label>
                    ))}
                </div>
              </div>
            </div>

            <div
              style={{
                border: "1px solid " + C.border,
                padding: "0.6rem",
                marginTop: "0.6rem",
                background: C.card,
              }}
            >
              <div style={{ ...labelStyle, marginBottom: "0.35rem" }}>Preview</div>
              {x && ys.length ? (
                <ChartView
                  chart={{ type: type, x_column: x, y_columns: ys }}
                  rows={preview.rows}
                  height={240}
                />
              ) : (
                <div style={{ color: C.muted, fontSize: "0.8rem", padding: "1rem" }}>
                  Pick an X column and at least one Y value.
                </div>
              )}
            </div>
          </div>
        )
      ) : null}

      {saveErr ? (
        <div style={{ marginTop: "0.7rem" }}>
          <ErrorText>{saveErr}</ErrorText>
        </div>
      ) : null}

      <div
        style={{ display: "flex", justifyContent: "flex-end", gap: "0.5rem", marginTop: "1rem" }}
      >
        <Button onClick={props.onClose}>Cancel</Button>
        <Button variant="primary" onClick={save} disabled={saving}>
          {saving ? "Saving…" : "Save chart"}
        </Button>
      </div>
    </Modal>
  );
}

// ── Report editor ─────────────────────────────────────────────────

export function ReportEditor(props) {
  const [title, setTitle] = React.useState("");
  const [blocks, setBlocks] = React.useState([]);
  const [charts, setCharts] = React.useState([]);
  const [saving, setSaving] = React.useState(false);
  const [err, setErr] = React.useState("");

  React.useEffect(() => {
    api
      .listCharts()
      .then((cs) => setCharts(cs || []))
      .catch(() => setCharts([]));
  }, []);

  function update(i, patch) {
    setBlocks((bs) => bs.map((b, j) => (j === i ? { ...b, ...patch } : b)));
  }
  function remove(i) {
    setBlocks((bs) => bs.filter((_, j) => j !== i));
  }
  function move(i, dir) {
    setBlocks((bs) => {
      const j = i + dir;
      if (j < 0 || j >= bs.length) return bs;
      const next = bs.slice();
      const tmp = next[i];
      next[i] = next[j];
      next[j] = tmp;
      return next;
    });
  }

  function save() {
    setErr("");
    if (!title.trim()) {
      setErr("A title is required.");
      return;
    }
    const payload = blocks.map((b) =>
      b.kind === "text"
        ? { kind: "text", text: b.text || "" }
        : { kind: "chart", chart_id: b.chartId || "" },
    );
    if (payload.some((b) => b.kind === "chart" && !b.chart_id)) {
      setErr("Every chart block needs a chart selected.");
      return;
    }
    setSaving(true);
    api
      .createReport({ title: title.trim(), blocks: payload })
      .then((r) => props.onSaved(r))
      .catch((e) => {
        setErr(String((e && e.message) || e));
        setSaving(false);
      });
  }

  return (
    <Modal title="New report" wide onClose={props.onClose}>
      <Field label="Title">
        <TextInput value={title} onChange={setTitle} placeholder="e.g. Q2 review" />
      </Field>

      <div style={labelStyle}>Blocks</div>
      {blocks.length === 0 ? (
        <div style={{ color: C.muted, fontSize: "0.8rem", marginBottom: "0.6rem" }}>
          No blocks yet — add narrative text and charts below.
        </div>
      ) : null}

      {blocks.map((b, i) => (
        <div
          key={i}
          style={{ border: "1px solid " + C.border, padding: "0.6rem", marginBottom: "0.5rem" }}
        >
          <div
            style={{
              display: "flex",
              justifyContent: "space-between",
              alignItems: "center",
              marginBottom: "0.4rem",
            }}
          >
            <Badge>{b.kind}</Badge>
            <div style={{ display: "flex", gap: "0.3rem" }}>
              <Button
                variant="ghost"
                onClick={() => move(i, -1)}
                disabled={i === 0}
                style={{ padding: "0.15rem 0.45rem" }}
              >
                ↑
              </Button>
              <Button
                variant="ghost"
                onClick={() => move(i, 1)}
                disabled={i === blocks.length - 1}
                style={{ padding: "0.15rem 0.45rem" }}
              >
                ↓
              </Button>
              <Button
                variant="danger"
                onClick={() => remove(i)}
                style={{ padding: "0.15rem 0.45rem" }}
              >
                Remove
              </Button>
            </div>
          </div>
          {b.kind === "text" ? (
            <TextArea
              value={b.text || ""}
              onChange={(v) => update(i, { text: v })}
              rows={3}
              placeholder="Markdown — # headings, **bold**, - lists…"
              style={{ fontFamily: "inherit" }}
            />
          ) : (
            <Select value={b.chartId || ""} onChange={(v) => update(i, { chartId: v })}>
              {charts.length === 0 ? <option value="">No charts available</option> : null}
              {charts.map((c) => (
                <option key={c.id} value={c.id}>
                  {c.title}
                </option>
              ))}
            </Select>
          )}
        </div>
      ))}

      <div style={{ display: "flex", gap: "0.5rem", marginTop: "0.3rem" }}>
        <Button onClick={() => setBlocks((bs) => bs.concat([{ kind: "text", text: "" }]))}>
          + Text
        </Button>
        <Button
          onClick={() =>
            setBlocks((bs) =>
              bs.concat([{ kind: "chart", chartId: charts[0] ? charts[0].id : "" }]),
            )
          }
          disabled={charts.length === 0}
        >
          + Chart
        </Button>
      </div>

      {err ? (
        <div style={{ marginTop: "0.7rem" }}>
          <ErrorText>{err}</ErrorText>
        </div>
      ) : null}

      <div
        style={{ display: "flex", justifyContent: "flex-end", gap: "0.5rem", marginTop: "1rem" }}
      >
        <Button onClick={props.onClose}>Cancel</Button>
        <Button variant="primary" onClick={save} disabled={saving}>
          {saving ? "Saving…" : "Save report"}
        </Button>
      </div>
    </Modal>
  );
}

// ── Name prompt (used to create a dashboard) ──────────────────────

export function NamePrompt(props) {
  const [name, setName] = React.useState("");
  const [busy, setBusy] = React.useState(false);
  const [err, setErr] = React.useState("");

  function submit() {
    if (!name.trim()) {
      setErr("A name is required.");
      return;
    }
    setBusy(true);
    setErr("");
    Promise.resolve(props.onSubmit(name.trim())).catch((e) => {
      setErr(String((e && e.message) || e));
      setBusy(false);
    });
  }

  return (
    <Modal title={props.title} onClose={props.onClose}>
      <Field label={props.label}>
        <TextInput value={name} onChange={setName} placeholder={props.placeholder} />
      </Field>
      {err ? <ErrorText>{err}</ErrorText> : null}
      <div
        style={{ display: "flex", justifyContent: "flex-end", gap: "0.5rem", marginTop: "1rem" }}
      >
        <Button onClick={props.onClose}>Cancel</Button>
        <Button variant="primary" onClick={submit} disabled={busy}>
          {busy ? "…" : "Create"}
        </Button>
      </div>
    </Modal>
  );
}
