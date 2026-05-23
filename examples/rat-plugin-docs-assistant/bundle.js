/*
 * rat-plugin-docs-assistant — portal UI bundle (Layer 3).
 *
 * Build-free: uses the portal's window.React. Registers a button into the
 * core "table-actions" slot of the table-detail page. Clicking the button
 * opens a modal that:
 *   1. Loads the table's current schema and descriptions from ratd
 *   2. Pulls a small data sample
 *   3. Asks the dev-assistant /suggest endpoint for {description,
 *      column_descriptions} — brokered to ai-provider through interconnect
 *   4. Lets the user edit and save through the core table-metadata API
 *      (PUT /api/v1/tables/{ns}/{layer}/{name}/metadata).
 */
(function () {
  "use strict";

  var React = window.React;
  if (!React || typeof window.__RAT_REGISTER_PLUGIN !== "function") {
    console.warn("[docs-assistant] RAT plugin host not available — skipping");
    return;
  }
  var h = React.createElement;

  var C = {
    border: "hsl(var(--border, 0 0% 16%))",
    fg: "hsl(var(--foreground, 0 0% 90%))",
    muted: "hsl(var(--muted-foreground, 0 0% 50%))",
    card: "hsl(var(--card, 0 0% 7%))",
    primary: "hsl(var(--primary, 142 72% 45%))",
    bg: "hsl(var(--background, 0 0% 4%))",
    danger: "hsl(var(--destructive, 0 62% 45%))",
  };

  function apiBase() {
    var s = document.querySelector('script[src*="/plugins/docs-assistant/ui/bundle.js"]');
    if (s && s.src && s.src.indexOf("/api/v1/") !== -1) {
      return s.src.slice(0, s.src.indexOf("/api/v1/"));
    }
    return window.location.origin;
  }
  var API = apiBase();

  function req(method, path, body) {
    return fetch(API + path, {
      method: method,
      headers: { "Content-Type": "application/json" },
      body: body !== undefined ? JSON.stringify(body) : undefined,
    }).then(function (r) {
      return r.text().then(function (t) {
        var d = null;
        try { d = t ? JSON.parse(t) : null; } catch (e) { d = { error: t }; }
        if (!d) d = {};
        if (!r.ok && !d.error) d.error = "request failed: " + r.status;
        return d;
      });
    });
  }

  function parseTablePath() {
    var m = (window.location.pathname || "").match(/^\/explorer\/([^/]+)\/([^/]+)\/([^/]+)/);
    return m ? { ns: m[1], layer: m[2], name: m[3] } : null;
  }

  function formatSample(d) {
    if (!d || !d.columns) return "";
    var cols = d.columns.map(function (c) { return c.name + " (" + (c.type || "?") + ")"; }).join(", ");
    var rows = (d.rows || []).slice(0, 10).map(function (r) { return JSON.stringify(r); }).join("\n");
    return "columns: " + cols + (rows ? "\nrows:\n" + rows : "");
  }

  var btn = {
    fontSize: "0.7rem", fontWeight: 600, padding: "0.25rem 0.6rem", cursor: "pointer",
    fontFamily: "inherit", border: "1px solid " + C.border, background: "transparent", color: C.fg,
  };
  var btnPrimary = Object.assign({}, btn, {
    background: C.primary,
    color: "hsl(var(--primary-foreground, 0 0% 2%))",
    borderColor: C.primary,
  });

  function DocsAssistantModal(props) {
    var tableRef = props.tableRef;
    var phaseS = React.useState("loading");
    var phase = phaseS[0], setPhase = phaseS[1];
    var errS = React.useState("");
    var errorMsg = errS[0], setErrorMsg = errS[1];
    var modelS = React.useState("");
    var model = modelS[0], setModel = modelS[1];
    var colsS = React.useState([]);
    var columns = colsS[0], setColumns = colsS[1];
    var descS = React.useState("");
    var draftDesc = descS[0], setDraftDesc = descS[1];
    var draftS = React.useState({});
    var draftCols = draftS[0], setDraftCols = draftS[1];

    function runSuggest() {
      setPhase("loading");
      setErrorMsg("");
      var tableUrl = "/api/v1/tables/" + tableRef.ns + "/" + tableRef.layer + "/" + tableRef.name;
      var loaded = { columns: [], description: "", columnDescriptions: {} };

      fetch(API + tableUrl)
        .then(function (r) { if (!r.ok) throw new Error("table not found (HTTP " + r.status + ")"); return r.json(); })
        .then(function (t) {
          loaded.columns = (t.columns || []).map(function (c) { return { name: c.name, type: c.type || "" }; });
          loaded.description = t.description || "";
          (t.columns || []).forEach(function (c) { loaded.columnDescriptions[c.name] = c.description || ""; });
          setColumns(loaded.columns);
          setDraftDesc(loaded.description);
          setDraftCols(Object.assign({}, loaded.columnDescriptions));
          return fetch(API + tableUrl + "/preview")
            .then(function (r) { return r.ok ? r.json() : null; })
            .catch(function () { return null; });
        })
        .then(function (preview) {
          return req("POST", "/api/v1/x/docs-assistant/suggest", {
            table: tableRef,
            columns: loaded.columns,
            current_description: loaded.description,
            current_column_descriptions: loaded.columnDescriptions,
            data_sample: preview ? formatSample(preview) : "",
          });
        })
        .then(function (s) {
          if (!s) { setErrorMsg("no response from the assistant"); setPhase("error"); return; }
          if (s.model) setModel(s.model);
          if (s.error) {
            setErrorMsg("AI: " + s.error);
            setPhase("ready");
            return;
          }
          if (s.description) setDraftDesc(s.description);
          if (s.column_descriptions) {
            var merged = Object.assign({}, loaded.columnDescriptions);
            Object.keys(s.column_descriptions).forEach(function (k) {
              var v = s.column_descriptions[k];
              if (typeof v === "string" && v.trim()) merged[k] = v;
            });
            setDraftCols(merged);
          }
          setPhase("ready");
        })
        .catch(function (e) {
          setErrorMsg(String((e && e.message) || e));
          setPhase("error");
        });
    }

    React.useEffect(runSuggest, []);

    function save() {
      setPhase("saving");
      setErrorMsg("");
      fetch(
        API + "/api/v1/tables/" + tableRef.ns + "/" + tableRef.layer + "/" + tableRef.name + "/metadata",
        {
          method: "PUT",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ description: draftDesc, column_descriptions: draftCols }),
        },
      )
        .then(function (r) {
          if (!r.ok) return r.text().then(function (t) { throw new Error("save failed: " + (t || r.status)); });
          // Tell the portal to refresh its SWR caches so the Explorer's
          // useTable() sees the new descriptions without a page reload.
          if (typeof window.__RAT_INVALIDATE === "function") {
            window.__RAT_INVALIDATE("table");
          }
          setPhase("saved");
          setTimeout(props.onClose, 800);
        })
        .catch(function (e) {
          setErrorMsg(String((e && e.message) || e));
          setPhase("ready");
        });
    }

    var fieldLabel = {
      fontSize: "0.62rem", fontWeight: 700, color: C.muted,
      textTransform: "uppercase", letterSpacing: "0.05em",
    };
    var input = {
      width: "100%", boxSizing: "border-box", padding: "0.4rem 0.5rem",
      fontFamily: "inherit", fontSize: "0.8rem",
      background: C.bg, color: C.fg, border: "1px solid " + C.border,
    };

    return h("div", {
        style: {
          position: "fixed", inset: 0, background: "rgba(0,0,0,0.55)", zIndex: 9999,
          display: "flex", alignItems: "center", justifyContent: "center", padding: "2rem",
        },
        onClick: function (e) { if (e.target === e.currentTarget) props.onClose(); },
      },
      h("div", {
          style: {
            background: C.card, border: "1px solid " + C.border,
            width: "min(46rem, 96vw)", maxHeight: "90vh",
            display: "flex", flexDirection: "column",
            boxShadow: "0 12px 40px rgba(0,0,0,0.5)",
          },
        },
        // header
        h("div", {
            style: {
              display: "flex", justifyContent: "space-between", alignItems: "center",
              padding: "0.6rem 0.9rem", borderBottom: "1px solid " + C.border,
            },
          },
          h("div", null,
            h("span", { style: { fontWeight: 800, fontSize: "0.92rem" } }, "🤖 Docs Assistant"),
            h("div", { style: { fontSize: "0.7rem", color: C.muted, marginTop: "0.15rem", fontFamily: "monospace" } },
              tableRef.ns + "." + tableRef.layer + "." + tableRef.name)),
          h("button", { onClick: props.onClose, style: Object.assign({}, btn, { padding: "0.15rem 0.5rem" }), title: "Close" }, "✕")),

        // body
        h("div", {
            style: {
              flex: 1, overflowY: "auto", padding: "0.9rem",
              display: "flex", flexDirection: "column", gap: "0.9rem",
            },
          },
          phase === "loading"
            ? h("div", { style: { color: C.muted, fontSize: "0.82rem" } }, "Loading table and asking the assistant…")
            : phase === "error"
              ? h("div", { style: { color: C.danger, fontSize: "0.82rem" } }, "✗ " + (errorMsg || "Something went wrong"))
              : h("div", { style: { display: "flex", flexDirection: "column", gap: "0.9rem" } },
                  errorMsg ? h("div", { style: { color: C.danger, fontSize: "0.78rem" } }, errorMsg) : null,
                  h("div", null,
                    h("div", { style: fieldLabel }, "Table description"),
                    h("textarea", {
                      value: draftDesc,
                      onChange: function (e) { setDraftDesc(e.target.value); },
                      rows: 3,
                      style: Object.assign({}, input, { resize: "vertical", marginTop: "0.25rem" }),
                    })),
                  h("div", null,
                    h("div", { style: fieldLabel }, "Column descriptions"),
                    h("div", { style: { display: "flex", flexDirection: "column", gap: "0.45rem", marginTop: "0.3rem" } },
                      columns.map(function (c) {
                        return h("div", {
                            key: c.name,
                            style: { display: "grid", gridTemplateColumns: "11rem 1fr", gap: "0.5rem", alignItems: "start" },
                          },
                          h("div", { style: { paddingTop: "0.35rem" } },
                            h("div", { style: { fontFamily: "monospace", fontSize: "0.78rem" } }, c.name),
                            h("div", { style: { fontSize: "0.62rem", color: C.muted } }, c.type)),
                          h("textarea", {
                            value: draftCols[c.name] || "",
                            onChange: function (e) {
                              var v = e.target.value;
                              setDraftCols(function (prev) {
                                var n = Object.assign({}, prev);
                                n[c.name] = v;
                                return n;
                              });
                            },
                            rows: 2,
                            style: Object.assign({}, input, { resize: "vertical", fontSize: "0.76rem" }),
                          }));
                      }))),
                  model ? h("div", { style: { fontSize: "0.62rem", color: C.muted } }, "model: " + model) : null)),

        // footer
        h("div", {
            style: {
              display: "flex", justifyContent: "space-between",
              padding: "0.55rem 0.9rem", borderTop: "1px solid " + C.border,
            },
          },
          h("button", {
            onClick: runSuggest,
            disabled: phase === "loading" || phase === "saving",
            style: btn,
          }, "↻ Regenerate"),
          h("div", { style: { display: "flex", gap: "0.4rem" } },
            h("button", { onClick: props.onClose, style: btn, disabled: phase === "saving" }, "Cancel"),
            h("button", {
              onClick: save, style: btnPrimary,
              disabled: phase === "loading" || phase === "saving" || phase === "error",
            }, phase === "saving" ? "Saving…" : phase === "saved" ? "✓ Saved" : "Save changes")))));
  }

  function DocsAssistantButton() {
    var openS = React.useState(false);
    var open = openS[0], setOpen = openS[1];
    var ref = parseTablePath();
    if (!ref) return null;
    return h(React.Fragment, null,
      h("button", {
        onClick: function () { setOpen(true); },
        style: btn,
        title: "Suggest a table description and per-column descriptions with AI",
      }, "🤖 Suggest docs"),
      open
        ? h(DocsAssistantModal, { tableRef: ref, onClose: function () { setOpen(false); } })
        : null);
  }

  window.__RAT_REGISTER_PLUGIN("docs-assistant", {
    slots: { "table-actions": [DocsAssistantButton] },
  });
  console.info("[docs-assistant] registered with the portal");
})();
