/*
 * rat-plugin-agents — portal UI bundle (Layer 3).
 *
 * A dedicated /x/agents page: CRUD over the agent catalog. Each agent is
 * a named persona the chat plugin can adopt — a system prompt, a tool
 * whitelist (the namespaced MCP tool names: docs__list_tables,
 * sql__run_query, ...), plus optional model/temperature overrides.
 *
 * Build-free: no JSX, no bundler. Uses window.React + the host portal's
 * __RAT_REGISTER_PLUGIN hook.
 */
(function () {
  "use strict";

  var React = window.React;
  if (!React || typeof window.__RAT_REGISTER_PLUGIN !== "function") {
    console.warn("[agents] RAT plugin host not available — skipping");
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
    cardAlt: "hsl(var(--muted, 0 0% 10%))",
    primary: "hsl(var(--primary, 142 72% 45%))",
    bg: "hsl(var(--background, 0 0% 4%))",
    danger: "hsl(var(--destructive, 0 62% 35%))",
    accent: "hsl(var(--accent, 280 60% 50%))",
  };

  function apiBase() {
    var s = document.querySelector('script[src*="/plugins/agents/ui/bundle.js"]');
    if (s && s.src && s.src.indexOf("/api/v1/") !== -1) {
      return s.src.slice(0, s.src.indexOf("/api/v1/"));
    }
    return window.location.origin;
  }
  var AGENTS_API = apiBase() + "/api/v1/x/agents";
  var CHAT_API = apiBase() + "/api/v1/x/chat";

  function req(method, path, body) {
    var opts = { method: method, headers: {} };
    if (body !== undefined) {
      opts.headers["Content-Type"] = "application/json";
      opts.body = JSON.stringify(body);
    }
    return fetch(AGENTS_API + path, opts).then(function (res) {
      return res.text().then(function (t) {
        var d = null;
        try { d = t ? JSON.parse(t) : null; } catch (e) { d = { error: t }; }
        if (!res.ok) throw new Error((d && d.error) || ("HTTP " + res.status));
        return d;
      });
    });
  }

  function reqTools() {
    return fetch(CHAT_API + "/tools").then(function (r) {
      return r.json();
    }).then(function (d) { return d.tools || []; });
  }

  // ── Tool picker — checklist sorted by server ───────────────────

  function ToolPicker(props) {
    var allTools = props.tools || [];
    var selected = props.selected || ["*"];
    var allSelected = selected.length === 1 && selected[0] === "*";
    var stExp = useState({}), expanded = stExp[0], setExpanded = stExp[1];

    function setAll() { props.onChange(["*"]); }
    function setNone() { props.onChange([]); }

    function toggle(nsName) {
      var next;
      if (allSelected) {
        // Switching from "all" to explicit: include everything except the toggled one.
        next = allTools.map(function (t) { return t.namespaced; }).filter(function (n) { return n !== nsName; });
      } else if (selected.indexOf(nsName) >= 0) {
        next = selected.filter(function (n) { return n !== nsName; });
      } else {
        next = selected.concat([nsName]);
      }
      props.onChange(next);
    }
    function isSelected(nsName) {
      return allSelected || selected.indexOf(nsName) >= 0;
    }

    // Group tools by server (the prefix before "__").
    var byServer = {};
    allTools.forEach(function (t) {
      var key = t.server || "?";
      (byServer[key] = byServer[key] || []).push(t);
    });
    var serverNames = Object.keys(byServer).sort();

    return h("div", { style: { fontSize: 12, color: C.fg } },
      h("div", { style: { display: "flex", gap: 8, marginBottom: 8, alignItems: "center" } },
        h("button", {
          onClick: setAll, type: "button",
          style: btnStyle(allSelected ? C.primary : "transparent", allSelected ? C.bg : C.fg),
        }, "all"),
        h("button", {
          onClick: setNone, type: "button",
          style: btnStyle("transparent", C.fg),
        }, "none"),
        h("span", { style: { color: C.muted, fontSize: 11 } },
          allSelected ? "(all tools enabled)" :
            (selected.length + " of " + allTools.length + " selected")),
      ),
      serverNames.map(function (srv) {
        var open = expanded[srv] !== false; // default open
        var tools = byServer[srv];
        var selectedHere = tools.filter(function (t) { return isSelected(t.namespaced); }).length;
        return h("div", { key: srv, style: { marginTop: 8 } },
          h("div", {
            style: { display: "flex", alignItems: "center", gap: 6, cursor: "pointer", color: C.accent, fontWeight: 700 },
            onClick: function () {
              var e = Object.assign({}, expanded); e[srv] = !open; setExpanded(e);
            },
          },
            open ? "▾" : "▸", srv,
            h("span", { style: { color: C.muted, fontWeight: 400, fontSize: 11 } },
              " (" + selectedHere + "/" + tools.length + ")"),
          ),
          open && h("div", { style: { marginLeft: 18, marginTop: 4 } },
            tools.map(function (t) {
              return h("label", {
                key: t.namespaced,
                style: { display: "flex", alignItems: "center", gap: 8, padding: "2px 0", cursor: "pointer" },
              },
                h("input", {
                  type: "checkbox", checked: isSelected(t.namespaced),
                  onChange: function () { toggle(t.namespaced); },
                  style: { accentColor: C.primary },
                }),
                h("span", { style: { fontFamily: "monospace", fontSize: 11 } }, t.namespaced),
                h("span", { style: { color: C.muted, fontSize: 11 } },
                  " — " + (t.description || "").slice(0, 60) + ((t.description || "").length > 60 ? "…" : "")),
              );
            }),
          ),
        );
      }),
    );
  }

  // ── Agent edit form ────────────────────────────────────────────

  function btnStyle(bg, fg) {
    return {
      padding: "4px 10px", background: bg, color: fg,
      border: "1px solid " + C.border, fontSize: 11, cursor: "pointer",
      letterSpacing: 0.5,
    };
  }

  function AgentForm(props) {
    var initial = props.agent || {
      id: "", name: "", icon: "sparkles", description: "",
      system_prompt: "", allowed_tools: ["*"], model: "", temperature: 0,
    };
    var st = useState(initial), agent = st[0], setAgent = st[1];
    function set(k, v) { setAgent(Object.assign({}, agent, k && (typeof k === "object" ? k : (function () { var o = {}; o[k] = v; return o; })()))); }

    function save() {
      var payload = Object.assign({}, agent);
      if (payload.temperature === "" || payload.temperature === undefined) payload.temperature = 0;
      payload.temperature = parseFloat(payload.temperature) || 0;
      props.onSave(payload);
    }

    return h("div", {
      style: { border: "1px solid " + C.border, padding: 16, background: C.card },
    },
      h("div", { style: { display: "grid", gridTemplateColumns: "1fr 1fr", gap: 12 } },
        labeled("Name", h("input", {
          value: agent.name, onChange: function (e) { set("name", e.target.value); },
          style: inputStyle(),
        })),
        labeled("Icon (lucide name)", h("input", {
          value: agent.icon, onChange: function (e) { set("icon", e.target.value); },
          placeholder: "sparkles / compass / users / calculator …",
          style: inputStyle(),
        })),
      ),
      labeled("Description", h("input", {
        value: agent.description, onChange: function (e) { set("description", e.target.value); },
        style: inputStyle(),
      })),
      labeled("System prompt", h("textarea", {
        value: agent.system_prompt, onChange: function (e) { set("system_prompt", e.target.value); },
        rows: 8, style: Object.assign({}, inputStyle(), { fontFamily: "monospace", fontSize: 12, resize: "vertical" }),
      })),
      h("div", { style: { display: "grid", gridTemplateColumns: "1fr 1fr", gap: 12 } },
        labeled("Model override (optional)", h("input", {
          value: agent.model || "", onChange: function (e) { set("model", e.target.value); },
          placeholder: "leave empty to use ai-provider default",
          style: inputStyle(),
        })),
        labeled("Temperature (0 = unset)", h("input", {
          type: "number", step: "0.05", min: "0", max: "2",
          value: agent.temperature || 0, onChange: function (e) { set("temperature", e.target.value); },
          style: inputStyle(),
        })),
      ),
      labeled("Allowed tools", h(ToolPicker, {
        tools: props.allTools, selected: agent.allowed_tools || ["*"],
        onChange: function (next) { set("allowed_tools", next); },
      })),
      h("div", { style: { display: "flex", gap: 8, justifyContent: "flex-end", marginTop: 12 } },
        h("button", { onClick: props.onCancel, type: "button", style: btnStyle("transparent", C.fg) }, "Cancel"),
        h("button", { onClick: save, type: "button", style: btnStyle(C.primary, C.bg) }, agent.id ? "Save" : "Create"),
      ),
    );
  }

  function labeled(label, child) {
    return h("div", { style: { marginTop: 10 } },
      h("div", { style: { color: C.muted, fontSize: 10, textTransform: "uppercase", letterSpacing: 1, marginBottom: 4 } }, label),
      child,
    );
  }

  function inputStyle() {
    return {
      width: "100%", padding: 6, background: C.bg, color: C.fg,
      border: "1px solid " + C.border, fontFamily: "inherit", fontSize: 13,
      boxSizing: "border-box",
    };
  }

  // ── Main component ────────────────────────────────────────────

  function AgentsApp() {
    var st1 = useState([]),    agents = st1[0], setAgents = st1[1];
    var st2 = useState([]),    tools = st2[0], setTools = st2[1];
    var st3 = useState(null),  editing = st3[0], setEditing = st3[1]; // agent being edited or {} for new
    var st4 = useState(null),  err = st4[0], setErr = st4[1];
    var st5 = useState(false), loading = st5[0], setLoading = st5[1];

    var refresh = useCallback(function () {
      setLoading(true);
      Promise.all([req("GET", "/agents"), reqTools()]).then(function (rs) {
        setAgents(rs[0].agents || []);
        setTools(rs[1] || []);
        setLoading(false);
      }).catch(function (e) {
        setErr(e.message || String(e));
        setLoading(false);
      });
    }, []);

    useEffect(refresh, [refresh]);

    function startCreate() { setEditing({}); setErr(null); }
    function startEdit(a) { setEditing(Object.assign({}, a)); setErr(null); }
    function cancelEdit() { setEditing(null); setErr(null); }

    function save(payload) {
      var p = payload.id
        ? req("PUT", "/agents/" + payload.id, payload)
        : req("POST", "/agents", payload);
      p.then(function () { setEditing(null); refresh(); })
       .catch(function (e) { setErr(e.message || String(e)); });
    }

    function remove(a) {
      if (!window.confirm("Delete agent \"" + a.name + "\"? This can't be undone.")) return;
      req("DELETE", "/agents/" + a.id).then(refresh).catch(function (e) { setErr(e.message); });
    }

    function seed() {
      req("POST", "/agents/seed").then(refresh).catch(function (e) { setErr(e.message); });
    }

    return h("div", {
      style: { padding: 20, color: C.fg, background: C.bg, minHeight: "calc(100vh - 60px)" },
    },
      h("div", { style: { display: "flex", alignItems: "center", gap: 12, marginBottom: 16 } },
        h("h1", { style: { margin: 0, fontSize: 16, letterSpacing: 1, fontWeight: 700 } }, "AGENTS"),
        h("span", { style: { color: C.muted, fontSize: 12 } },
          agents.length + " agent" + (agents.length === 1 ? "" : "s")),
        h("div", { style: { marginLeft: "auto", display: "flex", gap: 8 } },
          agents.length === 0 && h("button", { onClick: seed, style: btnStyle(C.accent, C.bg) }, "Seed defaults"),
          h("button", { onClick: startCreate, style: btnStyle(C.primary, C.bg) }, "+ New agent"),
        ),
      ),

      err && h("div", {
        style: { padding: 10, background: "rgba(239,68,68,0.10)", border: "1px solid " + C.danger, color: C.danger, fontSize: 12, marginBottom: 12 },
      }, "error: " + err),

      editing
        ? h(AgentForm, { agent: editing, allTools: tools, onSave: save, onCancel: cancelEdit })
        : h("div", { style: { display: "grid", gap: 10 } },
            loading && h("div", { style: { color: C.muted, fontSize: 12 } }, "loading…"),
            agents.length === 0 && !loading && h("div", { style: { color: C.muted, fontSize: 13 } },
              "No agents yet. Click ", h("strong", null, "Seed defaults"), " or ", h("strong", null, "+ New agent"), "."),
            agents.map(function (a) {
              var allowed = (a.allowed_tools && a.allowed_tools[0] === "*") ? "all tools" : (a.allowed_tools.length + " tools");
              return h("div", {
                key: a.id,
                style: { padding: 12, border: "1px solid " + C.border, background: C.card, display: "flex", gap: 12, alignItems: "flex-start" },
              },
                h("div", { style: { flex: 1, minWidth: 0 } },
                  h("div", { style: { display: "flex", alignItems: "baseline", gap: 8 } },
                    h("span", { style: { fontWeight: 700, fontSize: 14 } }, a.name),
                    h("span", { style: { color: C.muted, fontSize: 11, fontFamily: "monospace" } }, a.id),
                    h("span", { style: { color: C.muted, fontSize: 11 } }, " · " + allowed),
                    a.model && h("span", { style: { color: C.muted, fontSize: 11 } }, " · model=" + a.model),
                  ),
                  a.description && h("div", { style: { color: C.muted, fontSize: 12, marginTop: 4 } }, a.description),
                  a.system_prompt && h("div", {
                    style: { color: C.muted, fontSize: 11, marginTop: 6, fontStyle: "italic",
                             maxHeight: 60, overflow: "hidden", textOverflow: "ellipsis" },
                  }, a.system_prompt.slice(0, 240) + (a.system_prompt.length > 240 ? "…" : "")),
                ),
                h("div", { style: { display: "flex", flexDirection: "column", gap: 6 } },
                  h("button", { onClick: function () { startEdit(a); }, style: btnStyle("transparent", C.fg) }, "edit"),
                  h("button", { onClick: function () { remove(a); }, style: btnStyle("transparent", C.danger) }, "delete"),
                ),
              );
            }),
          ),
    );
  }

  window.__RAT_REGISTER_PLUGIN("agents", {
    navItems: [{ label: "Agents", icon: "users", href: "/x/agents", priority: 6 }],
    routes: [{ path: "/x/agents", component: AgentsApp }],
  });
  console.info("[agents] registered with the portal");
})();
