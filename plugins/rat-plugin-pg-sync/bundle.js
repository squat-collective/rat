/*
 * rat-plugin-pg-sync — portal UI bundle.
 *
 * Two tabs:
 *   Connections — name + secret pointer (the secret holds the URL).
 *   Tables       — per-table sync definitions, grouped by connection.
 *
 * Build-free (no JSX, no bundler) so it can sit alongside the Go plugin
 * and ship as a single Docker image.
 */
(function () {
  "use strict";

  var React = window.React;
  if (!React || typeof window.__RAT_REGISTER_PLUGIN !== "function") {
    console.warn("[pg-sync] RAT plugin host not available — skipping");
    return;
  }
  var h = React.createElement;
  var useState = React.useState;
  var useEffect = React.useEffect;
  var useCallback = React.useCallback;

  var C = {
    border: "hsl(var(--border, 0 0% 16%))",
    fg: "hsl(var(--foreground, 0 0% 90%))",
    muted: "hsl(var(--muted-foreground, 0 0% 50%))",
    card: "hsl(var(--card, 0 0% 7%))",
    bg: "hsl(var(--background, 0 0% 4%))",
    primary: "hsl(var(--primary, 142 72% 45%))",
    accent: "hsl(var(--accent, 280 50% 55%))",
    danger: "hsl(var(--destructive, 0 62% 35%))",
    warn: "hsl(var(--warning, 38 92% 50%))",
    ok: "hsl(142 60% 45%)",
  };

  function apiBase() {
    var s = document.querySelector('script[src*="/plugins/pg-sync/ui/bundle.js"]');
    if (s && s.src && s.src.indexOf("/api/v1/") !== -1) {
      return s.src.slice(0, s.src.indexOf("/api/v1/"));
    }
    return window.location.origin;
  }
  var ROOT = apiBase();
  var API = ROOT + "/api/v1/x/pg-sync";
  var SECRETS_API = ROOT + "/api/v1/x/secrets";

  function req(url, method, body) {
    var opts = { method: method || "GET", headers: {} };
    if (body !== undefined) {
      opts.headers["Content-Type"] = "application/json";
      opts.body = JSON.stringify(body);
    }
    return fetch(url, opts).then(function (res) {
      return res.text().then(function (t) {
        var d = null;
        try { d = t ? JSON.parse(t) : null; } catch (e) { d = { error: t }; }
        if (!res.ok) throw new Error((d && d.error) || ("HTTP " + res.status));
        return d;
      });
    });
  }

  var inputStyle = {
    width: "100%", padding: 6, background: C.bg, color: C.fg,
    border: "1px solid " + C.border, fontFamily: "inherit", fontSize: 13,
    boxSizing: "border-box",
  };
  var monoInputStyle = Object.assign({}, inputStyle, { fontFamily: "monospace" });
  function btn(bg, fg, extra) {
    return Object.assign({
      padding: "4px 10px", background: bg, color: fg,
      border: "1px solid " + C.border, fontSize: 11,
      cursor: "pointer", letterSpacing: 0.5, fontFamily: "inherit",
    }, extra || {});
  }
  function labeled(label, child, hint) {
    return h("div", { style: { marginTop: 10 } },
      h("div", { style: { color: C.muted, fontSize: 10, textTransform: "uppercase", letterSpacing: 1, marginBottom: 4 } }, label),
      child,
      hint && h("div", { style: { color: C.muted, fontSize: 10, marginTop: 3, fontStyle: "italic" } }, hint),
    );
  }
  function relativeTime(iso) {
    if (!iso) return "never";
    var diff = (Date.now() - new Date(iso).getTime()) / 1000;
    if (diff < 60) return Math.round(diff) + "s ago";
    if (diff < 3600) return Math.round(diff / 60) + "m ago";
    if (diff < 86400) return Math.round(diff / 3600) + "h ago";
    return Math.round(diff / 86400) + "d ago";
  }

  // ── Connections tab ────────────────────────────────────────────

  function ConnectionForm(props) {
    var s = useState(props.initial || { name: "", secret_name: "", description: "" });
    var form = s[0], setForm = s[1];
    var b = useState(false), busy = b[0], setBusy = b[1];
    var e = useState(null), err = e[0], setErr = e[1];

    function save() {
      setBusy(true); setErr(null);
      req(API + "/connections", "POST", form).then(function () {
        setBusy(false); props.onSaved();
      }).catch(function (er) { setErr(er.message); setBusy(false); });
    }

    return h("div", { style: { border: "1px solid " + C.border, padding: 16, background: C.card } },
      h("div", { style: { fontWeight: 700, fontSize: 13, marginBottom: 4 } },
        props.initial ? "Update connection" : "New connection"),
      labeled("Connection name",
        h("input", {
          value: form.name, disabled: !!props.initial,
          onChange: function (e) { setForm(Object.assign({}, form, { name: e.target.value.trim() })); },
          placeholder: "e.g. pg_prod", style: monoInputStyle,
        }),
        "Used as the alias in generated pipelines"),
      labeled("Secret name (from /x/secrets)",
        h("select", {
          value: form.secret_name,
          onChange: function (e) { setForm(Object.assign({}, form, { secret_name: e.target.value })); },
          style: monoInputStyle,
        },
          h("option", { value: "" }, "— select —"),
          (props.secrets || []).map(function (sec) {
            return h("option", { key: sec.name, value: sec.name }, sec.name);
          }),
        ),
        "The secret must contain a full postgresql:// URL"),
      labeled("Description (optional)",
        h("input", {
          value: form.description || "",
          onChange: function (e) { setForm(Object.assign({}, form, { description: e.target.value })); },
          style: inputStyle, placeholder: "What this connection is for",
        })),
      err && h("div", { style: { marginTop: 8, color: C.danger, fontSize: 11, padding: 6, background: "rgba(239,68,68,0.08)", border: "1px solid " + C.danger } }, err),
      h("div", { style: { display: "flex", justifyContent: "flex-end", gap: 6, marginTop: 12 } },
        h("button", { onClick: props.onCancel, style: btn("transparent", C.fg) }, "Cancel"),
        h("button", {
          onClick: save, disabled: busy || !form.name || !form.secret_name,
          style: btn(busy || !form.name || !form.secret_name ? C.muted : C.primary, C.bg),
        }, busy ? "saving…" : (props.initial ? "Save" : "Create")),
      ),
    );
  }

  function ConnectionsTab(props) {
    var s1 = useState(false), editing = s1[0], setEditing = s1[1];
    var s2 = useState({}), tests = s2[0], setTests = s2[1];

    function test(name) {
      setTests(Object.assign({}, tests, { [name]: { busy: true } }));
      req(API + "/connections/" + encodeURIComponent(name) + "/test", "POST").then(function (r) {
        setTests(function (prev) { var p = Object.assign({}, prev); p[name] = { ok: r.ok, detail: r.detail }; return p; });
      }).catch(function (er) {
        setTests(function (prev) { var p = Object.assign({}, prev); p[name] = { ok: false, detail: er.message }; return p; });
      });
    }
    function remove(name) {
      if (!window.confirm("Delete connection \"" + name + "\"? Any table syncs using it will be torn down too.")) return;
      req(API + "/connections/" + encodeURIComponent(name), "DELETE").then(props.refresh).catch(function (er) { alert(er.message); });
    }

    return h("div", null,
      !editing && h("div", { style: { textAlign: "right", marginBottom: 8 } },
        h("button", { onClick: function () { setEditing(true); }, style: btn(C.primary, C.bg) }, "+ New connection")),
      editing && h(ConnectionForm, {
        secrets: props.secrets,
        onSaved: function () { setEditing(false); props.refresh(); },
        onCancel: function () { setEditing(false); },
      }),

      !editing && (props.connections || []).length === 0 && h("div", {
        style: { padding: 16, color: C.muted, fontSize: 13, border: "1px dashed " + C.border },
      },
        "No connections yet. ",
        (props.secrets || []).length === 0
          ? h("span", null, "First, add a secret in ", h("a", { href: "/x/secrets", style: { color: C.primary } }, "/x/secrets"), " — it must hold a postgresql:// URL.")
          : "Click + New connection to point at one of your secrets.",
      ),

      !editing && (props.connections || []).map(function (c) {
        var t = tests[c.name];
        return h("div", { key: c.name,
          style: { padding: 12, border: "1px solid " + C.border, background: C.card, marginBottom: 8 },
        },
          h("div", { style: { display: "flex", alignItems: "center", gap: 12 } },
            h("div", { style: { flex: 1, minWidth: 0 } },
              h("div", { style: { fontFamily: "monospace", fontWeight: 700, fontSize: 14 } }, c.name),
              h("div", { style: { color: C.muted, fontSize: 11, marginTop: 2 } },
                "secret: ", h("code", null, c.secret_name)),
              c.description && h("div", { style: { fontSize: 11, marginTop: 4 } }, c.description),
            ),
            h("div", { style: { display: "flex", gap: 6 } },
              h("button", { onClick: function () { test(c.name); }, style: btn("transparent", C.fg) }, t && t.busy ? "…" : "test"),
              h("button", { onClick: function () { remove(c.name); }, style: btn("transparent", C.danger) }, "delete"),
            ),
          ),
          t && !t.busy && h("div", {
            style: { marginTop: 8, padding: 6, fontSize: 11,
                     color: t.ok ? C.ok : C.danger,
                     background: t.ok ? "rgba(34,197,94,0.07)" : "rgba(239,68,68,0.07)",
                     border: "1px solid " + (t.ok ? C.ok : C.danger) },
          }, (t.ok ? "✓ " : "✗ ") + t.detail),
        );
      }),
    );
  }

  // ── Tables tab ─────────────────────────────────────────────────

  function TableForm(props) {
    var defaults = Object.assign({
      connection: (props.connections[0] && props.connections[0].name) || "",
      source_schema: "public",
      source_table: "",
      target_namespace: "external",
      target_layer: "bronze",
      target_name: "",
      mode: "snapshot",
      watermark_column: "",
      primary_key: "",
      cron: "",
      enabled: true,
    }, props.initial || {});
    var s = useState(defaults), form = s[0], setForm = s[1];
    var b = useState(false), busy = b[0], setBusy = b[1];
    var e = useState(null), err = e[0], setErr = e[1];

    function set(k, v) { setForm(Object.assign({}, form, { [k]: v })); }
    function save() {
      setBusy(true); setErr(null);
      var url = props.initial ? API + "/tables/" + props.initial.id : API + "/tables";
      var method = props.initial ? "PUT" : "POST";
      req(url, method, form).then(function () {
        setBusy(false); props.onSaved();
      }).catch(function (er) { setErr(er.message); setBusy(false); });
    }

    var defaultCron = form.mode === "incremental" ? "*/30 * * * * *" : "0 */5 * * * *";

    return h("div", { style: { border: "1px solid " + C.border, padding: 16, background: C.card } },
      h("div", { style: { fontWeight: 700, fontSize: 13, marginBottom: 4 } },
        props.initial ? "Update table sync" : "New table sync"),
      labeled("Connection",
        h("select", { value: form.connection, onChange: function (e) { set("connection", e.target.value); }, style: monoInputStyle },
          (props.connections || []).map(function (c) {
            return h("option", { key: c.name, value: c.name }, c.name);
          }),
        )),
      h("div", { style: { display: "grid", gridTemplateColumns: "1fr 1fr", gap: 8 } },
        labeled("Source schema",
          h("input", { value: form.source_schema, onChange: function (e) { set("source_schema", e.target.value); }, style: monoInputStyle, placeholder: "public" })),
        labeled("Source table",
          h("input", { value: form.source_table, onChange: function (e) { set("source_table", e.target.value); }, style: monoInputStyle, placeholder: "country_codes" })),
      ),
      h("div", { style: { display: "grid", gridTemplateColumns: "1fr 1fr 1fr", gap: 8 } },
        labeled("Target namespace",
          h("input", { value: form.target_namespace, onChange: function (e) { set("target_namespace", e.target.value); }, style: monoInputStyle })),
        labeled("Target layer",
          h("select", { value: form.target_layer, onChange: function (e) { set("target_layer", e.target.value); }, style: monoInputStyle },
            ["bronze", "silver", "gold"].map(function (l) { return h("option", { key: l, value: l }, l); }))),
        labeled("Target name",
          h("input", { value: form.target_name, onChange: function (e) { set("target_name", e.target.value); }, style: monoInputStyle, placeholder: "(same as source)" })),
      ),
      labeled("Sync mode",
        h("div", { style: { display: "flex", gap: 6 } },
          ["snapshot", "incremental"].map(function (m) {
            var active = form.mode === m;
            return h("button", { key: m, onClick: function () { set("mode", m); },
              style: btn(active ? C.primary : "transparent", active ? C.bg : C.fg, { flex: 1 }) },
              m);
          })),
        form.mode === "snapshot"
          ? "Full refresh every run — simple but reads the whole source each time."
          : "Reads only rows where the watermark is greater than the max watermark already in the target."),
      form.mode === "incremental" && h("div", { style: { display: "grid", gridTemplateColumns: "1fr 1fr", gap: 8 } },
        labeled("Watermark column",
          h("input", { value: form.watermark_column, onChange: function (e) { set("watermark_column", e.target.value); }, style: monoInputStyle, placeholder: "e.g. updated_at" }),
          "Monotonic — created_at, updated_at, or a sequence id"),
        labeled("Primary key column",
          h("input", { value: form.primary_key, onChange: function (e) { set("primary_key", e.target.value); }, style: monoInputStyle, placeholder: "e.g. id" }),
          "Used as Iceberg unique_key for dedup on re-runs"),
      ),
      labeled("Cron (6-field, sub-minute supported)",
        h("input", { value: form.cron, onChange: function (e) { set("cron", e.target.value); }, style: monoInputStyle, placeholder: defaultCron }),
        "Leave empty for the default (" + defaultCron + ")"),
      labeled("Enabled",
        h("button", { onClick: function () { set("enabled", !form.enabled); },
          style: btn(form.enabled ? C.ok : "transparent", form.enabled ? C.bg : C.muted) },
          form.enabled ? "enabled" : "disabled")),
      err && h("div", { style: { marginTop: 8, color: C.danger, fontSize: 11, padding: 6, background: "rgba(239,68,68,0.08)", border: "1px solid " + C.danger } }, err),
      h("div", { style: { display: "flex", justifyContent: "flex-end", gap: 6, marginTop: 12 } },
        h("button", { onClick: props.onCancel, style: btn("transparent", C.fg) }, "Cancel"),
        h("button", {
          onClick: save,
          disabled: busy || !form.connection || !form.source_table || (form.mode === "incremental" && (!form.watermark_column || !form.primary_key)),
          style: btn((busy || !form.connection || !form.source_table) ? C.muted : C.primary, C.bg),
        }, busy ? "saving…" : (props.initial ? "Save" : "Create")),
      ),
    );
  }

  function TablesTab(props) {
    var s1 = useState(null), editing = s1[0], setEditing = s1[1]; // null | true | {row}
    var s2 = useState({}), syncing = s2[0], setSyncing = s2[1];

    function doSync(id) {
      setSyncing(Object.assign({}, syncing, { [id]: true }));
      req(API + "/tables/" + id + "/sync-now", "POST").then(function (r) {
        setSyncing(function (prev) { var p = Object.assign({}, prev); p[id] = false; return p; });
        if (r && r.run_id) {
          // Best-effort deep link to the run; harmless if the route doesn't exist.
          window.open("/runs/" + r.run_id, "_blank");
        }
        setTimeout(props.refresh, 1000);
      }).catch(function (er) {
        setSyncing(function (prev) { var p = Object.assign({}, prev); p[id] = false; return p; });
        alert(er.message);
      });
    }
    function remove(t) {
      if (!window.confirm("Delete sync \"" + t.target_namespace + "." + t.target_layer + "." + t.target_name + "\"? The pipeline and schedule will be torn down (the Iceberg table stays).")) return;
      req(API + "/tables/" + t.id, "DELETE").then(props.refresh).catch(function (er) { alert(er.message); });
    }

    var noConnections = (props.connections || []).length === 0;
    var emptyState = noConnections
      ? h("span", null, "First, add a connection in the ", h("strong", null, "Connections"), " tab.")
      : "No table syncs yet. Click + New table sync to mirror an external Postgres table.";

    return h("div", null,
      !editing && h("div", { style: { textAlign: "right", marginBottom: 8 } },
        h("button", {
          onClick: function () { setEditing(true); },
          disabled: noConnections,
          style: btn(noConnections ? C.muted : C.primary, C.bg),
        }, "+ New table sync")),
      editing && h(TableForm, {
        connections: props.connections,
        initial: editing === true ? null : editing,
        onSaved: function () { setEditing(null); props.refresh(); },
        onCancel: function () { setEditing(null); },
      }),

      !editing && (props.tables || []).length === 0 && h("div", {
        style: { padding: 16, color: C.muted, fontSize: 13, border: "1px dashed " + C.border },
      }, emptyState),

      !editing && (props.tables || []).map(function (t) {
        return h("div", { key: t.id,
          style: { padding: 12, border: "1px solid " + C.border, background: C.card, marginBottom: 8 },
        },
          h("div", { style: { display: "flex", alignItems: "center", gap: 12 } },
            h("div", { style: { flex: 1, minWidth: 0 } },
              h("div", { style: { fontSize: 13 } },
                h("code", { style: { fontWeight: 700 } }, t.connection),
                "  ",
                h("span", { style: { color: C.muted } }, t.source_schema + "." + t.source_table),
                "  →  ",
                h("code", { style: { fontWeight: 700, color: C.accent } }, t.target_namespace + "." + t.target_layer + "." + t.target_name),
              ),
              h("div", { style: { color: C.muted, fontSize: 11, marginTop: 4, display: "flex", gap: 12, flexWrap: "wrap" } },
                h("span", null, "mode: ", h("strong", null, t.mode)),
                t.watermark_column && h("span", null, "watermark: ", h("code", null, t.watermark_column)),
                h("span", null, "cron: ", h("code", null, t.cron)),
                h("span", null, "last sync: ", relativeTime(t.last_synced_at)),
                !t.enabled && h("span", { style: { color: C.warn } }, "● disabled"),
              ),
              t.last_error && h("div", { style: { color: C.danger, fontSize: 11, marginTop: 6 } }, "last error: " + t.last_error),
            ),
            h("div", { style: { display: "flex", gap: 6 } },
              h("button", { onClick: function () { doSync(t.id); }, disabled: syncing[t.id], style: btn(syncing[t.id] ? C.muted : C.ok, C.bg) }, syncing[t.id] ? "…" : "sync now"),
              h("button", { onClick: function () { setEditing(t); }, style: btn("transparent", C.fg) }, "edit"),
              h("button", { onClick: function () { remove(t); }, style: btn("transparent", C.danger) }, "delete"),
            ),
          ),
        );
      }),
    );
  }

  // ── App shell ──────────────────────────────────────────────────

  function PgSyncApp() {
    var s1 = useState("connections"), tab = s1[0], setTab = s1[1];
    var s2 = useState([]), connections = s2[0], setConnections = s2[1];
    var s3 = useState([]), tables = s3[0], setTables = s3[1];
    var s4 = useState([]), secrets = s4[0], setSecrets = s4[1];
    var s5 = useState(null), err = s5[0], setErr = s5[1];

    var refresh = useCallback(function () {
      setErr(null);
      Promise.all([
        req(API + "/connections"), req(API + "/tables"), req(SECRETS_API + "/secrets"),
      ]).then(function (r) {
        setConnections((r[0] && r[0].connections) || []);
        setTables((r[1] && r[1].tables) || []);
        setSecrets((r[2] && r[2].secrets) || []);
      }).catch(function (e) { setErr(e.message); });
    }, []);

    useEffect(refresh, [refresh]);

    function tabBtn(id, label, count) {
      var active = tab === id;
      return h("button", {
        onClick: function () { setTab(id); },
        style: btn(active ? C.primary : "transparent", active ? C.bg : C.fg, { fontWeight: active ? 700 : 400 }),
      }, label + (count !== undefined ? "  (" + count + ")" : ""));
    }

    return h("div", { style: { padding: 20, color: C.fg, background: C.bg, minHeight: "calc(100vh - 60px)" } },
      h("div", { style: { display: "flex", alignItems: "center", gap: 12, marginBottom: 16 } },
        h("h1", { style: { margin: 0, fontSize: 16, letterSpacing: 1, fontWeight: 700 } }, "PG SYNC"),
        h("span", { style: { color: C.muted, fontSize: 12 } }, "external postgres → iceberg"),
      ),

      h("div", { style: { display: "flex", gap: 6, marginBottom: 14, borderBottom: "1px solid " + C.border, paddingBottom: 8 } },
        tabBtn("connections", "connections", connections.length),
        tabBtn("tables", "tables", tables.length),
      ),

      err && h("div", { style: { padding: 10, marginBottom: 12, background: "rgba(239,68,68,0.10)", border: "1px solid " + C.danger, color: C.danger, fontSize: 12 } }, "error: " + err),

      tab === "connections"
        ? h(ConnectionsTab, { connections: connections, secrets: secrets, refresh: refresh })
        : h(TablesTab, { tables: tables, connections: connections, refresh: refresh }),
    );
  }

  window.__RAT_REGISTER_PLUGIN("pg-sync", {
    navItems: [{ label: "Pg Sync", icon: "database", href: "/x/pg-sync", priority: 13 }],
    routes: [{ path: "/x/pg-sync", component: PgSyncApp }],
  });
  console.info("[pg-sync] registered with the portal");
})();
