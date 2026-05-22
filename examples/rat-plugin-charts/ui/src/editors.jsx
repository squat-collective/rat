// Component editors — one modal that edits the props of any component type.
// The chart editor is the rich one (type, SQL, axes, full appearance, live
// preview); the others are short forms.

import React from "react";
import { api } from "./api.js";
import { ChartView, seriesColors, PALETTE_NAMES } from "./chart.jsx";
import {
  C,
  Modal,
  Field,
  TextInput,
  TextArea,
  Select,
  Button,
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
const subLabel = { ...labelStyle, fontSize: "0.62rem", marginBottom: "0.2rem" };

// SqlField renders the portal's CodeMirror SQL editor (schema-aware
// autocomplete, syntax highlighting) when the host exposes it on
// window.__RAT_UI, and falls back to a plain textarea otherwise.
function SqlField(props) {
  const kit = typeof window !== "undefined" ? window.__RAT_UI : null;
  const Editor = kit && kit.SqlEditorWithSchema;
  if (Editor) {
    return (
      <Editor
        value={props.value || ""}
        onChange={props.onChange}
        onExecute={props.onExecute}
      />
    );
  }
  return (
    <TextArea
      value={props.value || ""}
      onChange={props.onChange}
      rows={props.rows || 5}
      placeholder={props.placeholder}
    />
  );
}

// A readable title for a component, used in pickers.
export function cmpTitle(c) {
  const p = c.props || {};
  if (c.type === "chart") return p.title || "Untitled chart";
  if (c.type === "metric") return p.label || "Untitled metric";
  if (c.type === "heading") return p.text || "Heading";
  if (c.type === "markdown") return "Text block";
  if (c.type === "ai") return p.prompt ? "AI: " + p.prompt.slice(0, 30) : "AI analysis";
  return c.type;
}

// ── Shared appearance controls ────────────────────────────────────

function Toggle(props) {
  return (
    <label
      style={{ display: "flex", alignItems: "center", gap: "0.35rem", fontSize: "0.78rem" }}
    >
      <input
        type="checkbox"
        checked={!!props.checked}
        onChange={(e) => props.onChange(e.target.checked)}
      />
      {props.label}
    </label>
  );
}

// ChartAppearance edits a ChartOptions object — only the controls relevant to
// the selected chart type are shown.
function ChartAppearance(props) {
  const { type, ys, options } = props;
  const opts = options || {};
  const multi = ys.length > 1;
  const set = (key, val) => props.onChange({ ...opts, [key]: val });
  function setColor(i, hex) {
    const colors = (opts.colors || []).slice();
    colors[i] = hex;
    props.onChange({ ...opts, colors: colors });
  }
  const resolved = seriesColors(opts, Math.max(1, ys.length));

  return (
    <div style={{ border: "1px solid " + C.border, padding: "0.65rem", marginTop: "0.6rem" }}>
      <div style={labelStyle}>Appearance</div>
      <div
        style={{ display: "flex", flexWrap: "wrap", gap: "0.8rem 1.3rem", alignItems: "flex-end" }}
      >
        <div>
          <div style={subLabel}>Palette</div>
          <Select
            value={opts.palette || "rat"}
            onChange={(v) => set("palette", v)}
            style={{ minWidth: "8rem" }}
          >
            {PALETTE_NAMES.map((p) => (
              <option key={p} value={p}>
                {p}
              </option>
            ))}
          </Select>
        </div>

        {type !== "pie" ? (
          <div>
            <div style={subLabel}>Series colours</div>
            <div style={{ display: "flex", gap: "0.3rem" }}>
              {ys.map((y, i) => (
                <input
                  key={y}
                  type="color"
                  title={y}
                  value={resolved[i] || "#4ade80"}
                  onChange={(e) => setColor(i, e.target.value)}
                  style={{
                    width: "2.1rem",
                    height: "1.8rem",
                    padding: 0,
                    border: "1px solid " + C.border,
                    background: "transparent",
                    cursor: "pointer",
                  }}
                />
              ))}
            </div>
          </div>
        ) : null}

        {type === "line" || type === "area" ? (
          <div>
            <div style={subLabel}>Curve</div>
            <Select value={opts.curve || "smooth"} onChange={(v) => set("curve", v)}>
              <option value="smooth">Smooth</option>
              <option value="linear">Linear</option>
              <option value="step">Step</option>
            </Select>
          </div>
        ) : null}

        {type === "bar" ? (
          <div>
            <div style={subLabel}>Bar radius · {opts.bar_radius || 0}</div>
            <input
              type="range"
              min={0}
              max={16}
              value={opts.bar_radius || 0}
              onChange={(e) => set("bar_radius", Number(e.target.value))}
            />
          </div>
        ) : null}

        {type === "pie" ? (
          <div>
            <div style={subLabel}>Donut hole · {opts.inner_radius || 0}%</div>
            <input
              type="range"
              min={0}
              max={80}
              value={opts.inner_radius || 0}
              onChange={(e) => set("inner_radius", Number(e.target.value))}
            />
          </div>
        ) : null}
      </div>

      <div
        style={{ display: "flex", flexWrap: "wrap", gap: "0.5rem 1.2rem", marginTop: "0.7rem" }}
      >
        {(type === "bar" || type === "area") && multi ? (
          <Toggle label="Stacked" checked={opts.stacked} onChange={(v) => set("stacked", v)} />
        ) : null}
        {type === "bar" ? (
          <Toggle
            label="Horizontal"
            checked={opts.horizontal}
            onChange={(v) => set("horizontal", v)}
          />
        ) : null}
        {type === "line" ? (
          <Toggle label="Dots" checked={opts.dots} onChange={(v) => set("dots", v)} />
        ) : null}
        <Toggle
          label="Data labels"
          checked={opts.show_labels}
          onChange={(v) => set("show_labels", v)}
        />
        {type !== "pie" ? (
          <Toggle label="Grid" checked={!opts.hide_grid} onChange={(v) => set("hide_grid", !v)} />
        ) : null}
        <Toggle
          label="Legend"
          checked={!opts.hide_legend}
          onChange={(v) => set("hide_legend", !v)}
        />
      </div>
    </div>
  );
}

// ── Chart fields ──────────────────────────────────────────────────

function ChartFields(props) {
  const v = props.value || {};
  const set = (patch) => props.onChange({ ...v, ...patch });
  const [preview, setPreview] = React.useState(null); // { rows } | { error }
  const [running, setRunning] = React.useState(false);

  function run() {
    if (!v.sql || !v.sql.trim() || running) return;
    setRunning(true);
    setPreview(null);
    api
      .query(v.sql)
      .then((res) => {
        if (res.error) {
          setPreview({ error: res.error });
          return;
        }
        const rows = res.rows || [];
        setPreview({ rows: rows });
        if (rows.length) {
          const cols = Object.keys(rows[0]);
          const patch = {};
          if (!v.x_column || cols.indexOf(v.x_column) === -1) patch.x_column = cols[0];
          if (!v.y_columns || !v.y_columns.length) {
            const numeric = cols.filter((k) => k !== cols[0] && typeof rows[0][k] === "number");
            patch.y_columns = numeric.length ? [numeric[0]] : cols.length > 1 ? [cols[1]] : [];
          }
          if (Object.keys(patch).length) props.onChange({ ...v, ...patch });
        }
      })
      .catch((e) => setPreview({ error: String((e && e.message) || e) }))
      .then(() => setRunning(false));
  }

  // Auto-run when editing a chart that already has SQL.
  React.useEffect(() => {
    if (v.sql && v.sql.trim()) run();
    // eslint-disable-next-line
  }, []);

  const cols = preview && preview.rows && preview.rows.length ? Object.keys(preview.rows[0]) : [];
  const ys = v.y_columns || [];
  function toggleY(c) {
    set({ y_columns: ys.indexOf(c) !== -1 ? ys.filter((y) => y !== c) : ys.concat([c]) });
  }

  return (
    <div>
      <Field label="Title">
        <TextInput
          value={v.title || ""}
          onChange={(t) => set({ title: t })}
          placeholder="e.g. Orders by customer"
        />
      </Field>
      <Field label="Chart type">
        <Select
          value={v.chart_type || "bar"}
          onChange={(t) => set({ chart_type: t })}
          style={{ maxWidth: "12rem" }}
        >
          <option value="bar">Bar</option>
          <option value="line">Line</option>
          <option value="area">Area</option>
          <option value="pie">Pie</option>
          <option value="radar">Radar</option>
        </Select>
      </Field>
      <Field
        label="SQL query"
        hint="Read-only SELECT — Ctrl-Enter runs it. Tables are namespace.layer.name."
      >
        <SqlField
          value={v.sql || ""}
          onChange={(s) => set({ sql: s })}
          onExecute={run}
          rows={5}
          placeholder={"SELECT name, sum(amount) AS total\nFROM default.bronze.orders\nGROUP BY name"}
        />
      </Field>
      <div style={{ marginBottom: "0.75rem" }}>
        <Button onClick={run} disabled={running || !v.sql || !v.sql.trim()}>
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
                <div style={subLabel}>X axis</div>
                <Select value={v.x_column || ""} onChange={(c) => set({ x_column: c })}>
                  {cols.map((c) => (
                    <option key={c} value={c}>
                      {c}
                    </option>
                  ))}
                </Select>
              </div>
              <div style={{ flex: 1, minWidth: "12rem" }}>
                <div style={subLabel}>Y values (series)</div>
                <div style={{ display: "flex", flexWrap: "wrap", gap: "0.3rem 0.8rem" }}>
                  {cols
                    .filter((c) => c !== v.x_column)
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

            <ChartAppearance
              type={v.chart_type || "bar"}
              ys={ys}
              options={v.options || {}}
              onChange={(o) => set({ options: o })}
            />

            <div
              style={{
                border: "1px solid " + C.border,
                padding: "0.6rem",
                marginTop: "0.6rem",
                background: C.card,
              }}
            >
              <div style={{ ...labelStyle, marginBottom: "0.35rem" }}>Preview</div>
              {v.x_column && ys.length ? (
                <ChartView
                  chart={{
                    type: v.chart_type || "bar",
                    x_column: v.x_column,
                    y_columns: ys,
                    options: v.options || {},
                  }}
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
    </div>
  );
}

// ── Heading / Markdown / Metric / AI fields ───────────────────────

function HeadingFields(props) {
  const v = props.value || {};
  const set = (patch) => props.onChange({ ...v, ...patch });
  return (
    <div>
      <Field label="Heading text">
        <TextInput value={v.text || ""} onChange={(t) => set({ text: t })} placeholder="Section title" />
      </Field>
      <Field label="Size">
        <Select
          value={String(v.level || 1)}
          onChange={(l) => set({ level: Number(l) })}
          style={{ maxWidth: "12rem" }}
        >
          <option value="1">Large</option>
          <option value="2">Medium</option>
          <option value="3">Small</option>
        </Select>
      </Field>
    </div>
  );
}

function MarkdownFields(props) {
  const v = props.value || {};
  return (
    <Field label="Markdown" hint="# headings, **bold**, - lists, `code`">
      <TextArea
        value={v.markdown || ""}
        onChange={(m) => props.onChange({ ...v, markdown: m })}
        rows={9}
        style={{ fontFamily: "inherit" }}
        placeholder={"## Notes\nWrite anything here."}
      />
    </Field>
  );
}

function MetricFields(props) {
  const v = props.value || {};
  const set = (patch) => props.onChange({ ...v, ...patch });
  const [test, setTest] = React.useState(null);
  const [busy, setBusy] = React.useState(false);

  function runTest() {
    if (!v.sql || !v.sql.trim() || busy) return;
    setBusy(true);
    setTest(null);
    api
      .query(v.sql)
      .then((res) => {
        if (res.error) setTest({ error: res.error });
        else if (!res.rows || !res.rows.length) setTest({ error: "no rows" });
        else {
          const row = res.rows[0];
          const k = Object.keys(row)[0];
          setTest({ value: String(row[k]) });
        }
      })
      .catch((e) => setTest({ error: String((e && e.message) || e) }))
      .then(() => setBusy(false));
  }

  return (
    <div>
      <Field label="Label">
        <TextInput
          value={v.label || ""}
          onChange={(l) => set({ label: l })}
          placeholder="e.g. Total revenue"
        />
      </Field>
      <Field
        label="SQL query"
        hint="The first value of the first row is shown — Ctrl-Enter tests it."
      >
        <SqlField
          value={v.sql || ""}
          onChange={(s) => set({ sql: s })}
          onExecute={runTest}
          rows={3}
          placeholder="SELECT sum(amount) FROM default.bronze.orders"
        />
      </Field>
      <div style={{ display: "flex", gap: "1.25rem", flexWrap: "wrap" }}>
        <Field label="Unit (optional)">
          <TextInput
            value={v.unit || ""}
            onChange={(u) => set({ unit: u })}
            placeholder="€, %, rows…"
            style={{ maxWidth: "8rem" }}
          />
        </Field>
        <div>
          <div style={subLabel}>Colour</div>
          <input
            type="color"
            value={v.color || "#4ade80"}
            onChange={(e) => set({ color: e.target.value })}
            style={{
              width: "3rem",
              height: "1.9rem",
              padding: 0,
              border: "1px solid " + C.border,
              background: "transparent",
              cursor: "pointer",
            }}
          />
        </div>
      </div>
      <div style={{ marginTop: "0.4rem" }}>
        <Button onClick={runTest} disabled={busy || !v.sql || !v.sql.trim()}>
          {busy ? "Testing…" : "▶ Test"}
        </Button>
        {test && test.error ? (
          <span style={{ color: C.danger, fontSize: "0.78rem", marginLeft: "0.6rem" }}>
            {test.error}
          </span>
        ) : null}
        {test && test.value !== undefined ? (
          <span style={{ color: C.primary, fontSize: "0.9rem", fontWeight: 700, marginLeft: "0.6rem" }}>
            = {test.value}
          </span>
        ) : null}
      </div>
    </div>
  );
}

function AIFields(props) {
  const v = props.value || {};
  // Changing the prompt or source clears the cached analysis so it regenerates.
  const set = (patch) => props.onChange({ ...v, ...patch, analysis: "" });
  const sources = (props.components || []).filter(
    (c) => c.type === "chart" || c.type === "metric",
  );
  return (
    <div>
      <Field label="Prompt" hint="What should the AI analyse or explain?">
        <TextArea
          value={v.prompt || ""}
          onChange={(p) => set({ prompt: p })}
          rows={3}
          style={{ fontFamily: "inherit" }}
          placeholder="Summarise the key trends and call out anything surprising."
        />
      </Field>
      <Field
        label="Analyse which component?"
        hint="The AI receives that component's data as context."
      >
        <Select value={v.source || ""} onChange={(s) => set({ source: s })}>
          <option value="">— none (answer the prompt only) —</option>
          {sources.map((c) => (
            <option key={c.id} value={c.id}>
              {cmpTitle(c)}
            </option>
          ))}
        </Select>
      </Field>
    </div>
  );
}

// ── Component editor modal ────────────────────────────────────────

const EDITOR_TITLE = {
  chart: "chart",
  heading: "heading",
  markdown: "text block",
  metric: "metric",
  ai: "AI analysis",
};

// ComponentEditor edits one component's props. onSave receives the new props
// object; the caller decides whether that means "add" or "update".
export function ComponentEditor(props) {
  const { component } = props;
  const [p, setP] = React.useState(component.props || {});

  let fields;
  if (component.type === "chart") fields = <ChartFields value={p} onChange={setP} />;
  else if (component.type === "heading") fields = <HeadingFields value={p} onChange={setP} />;
  else if (component.type === "markdown") fields = <MarkdownFields value={p} onChange={setP} />;
  else if (component.type === "metric") fields = <MetricFields value={p} onChange={setP} />;
  else if (component.type === "ai")
    fields = <AIFields value={p} onChange={setP} components={props.components} />;

  return (
    <Modal
      title={(props.isNew ? "Add " : "Edit ") + (EDITOR_TITLE[component.type] || component.type)}
      wide={component.type === "chart"}
      onClose={props.onClose}
    >
      {fields}
      <div
        style={{ display: "flex", justifyContent: "flex-end", gap: "0.5rem", marginTop: "1rem" }}
      >
        <Button onClick={props.onClose}>Cancel</Button>
        <Button variant="primary" onClick={() => props.onSave(p)}>
          {props.isNew ? "Add to dashboard" : "Save"}
        </Button>
      </div>
    </Modal>
  );
}

// ── Name prompt (create a dashboard) ──────────────────────────────

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
