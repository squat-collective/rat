/*
 * rat-plugin-interconnect — portal UI bundle (Layer 3): the Plugin Mesh page.
 *
 * Build-free: uses the portal's window.React and the window.__RAT_REGISTER_PLUGIN
 * hook. Draws the live plugin mesh (an SVG graph of plugins + capability
 * wiring), lists registered capabilities, and lets you invoke or register them.
 */
(function () {
  "use strict";

  var React = window.React;
  if (!React || typeof window.__RAT_REGISTER_PLUGIN !== "function") {
    console.warn("[interconnect] RAT plugin host not available — skipping");
    return;
  }
  var h = React.createElement;

  // Theme colours (wrapped in hsl() — the portal's CSS vars are bare triplets).
  var C = {
    border: "hsl(var(--border, 0 0% 16%))",
    fg: "hsl(var(--foreground, 0 0% 90%))",
    muted: "hsl(var(--muted-foreground, 0 0% 50%))",
    card: "hsl(var(--card, 0 0% 7%))",
    surface: "hsl(var(--secondary, 0 0% 12%))",
    primary: "hsl(var(--primary, 142 72% 45%))",
    bg: "hsl(var(--background, 0 0% 4%))",
    danger: "hsl(var(--destructive, 0 62% 35%))",
  };
  // SVG attributes don't resolve CSS vars — use hex for the graph.
  var GREEN = "#4ade80", RED = "#f87171", GREY = "#6b7280", LINE = "#8a8a8a";

  function apiBase() {
    var s = document.querySelector('script[src*="/plugins/interconnect/ui/bundle.js"]');
    if (s && s.src && s.src.indexOf("/api/v1/") !== -1) {
      return s.src.slice(0, s.src.indexOf("/api/v1/"));
    }
    return window.location.origin;
  }
  var API = apiBase() + "/api/v1/x/interconnect";

  function req(method, path, body) {
    var opts = { method: method, headers: {} };
    if (body !== undefined) {
      opts.headers["Content-Type"] = "application/json";
      opts.body = JSON.stringify(body);
    }
    return fetch(API + path, opts).then(function (res) {
      if (res.status === 204) return null;
      return res.text().then(function (t) {
        var d = null;
        try {
          d = t ? JSON.parse(t) : null;
        } catch (e) {
          d = { error: t };
        }
        if (!res.ok) throw new Error((d && d.error) || "request failed: " + res.status);
        return d;
      });
    });
  }

  // ── styles ────────────────────────────────────────────────────────
  var cardStyle = { border: "1px solid " + C.border, background: C.card, padding: "0.85rem" };
  function btn(variant) {
    var s = {
      fontSize: "0.74rem", fontWeight: 600, padding: "0.3rem 0.7rem", cursor: "pointer",
      fontFamily: "inherit", border: "1px solid " + C.border, background: "transparent", color: C.fg,
    };
    if (variant === "primary") {
      s.background = C.primary;
      s.color = "hsl(var(--primary-foreground, 0 0% 2%))";
      s.borderColor = C.primary;
    }
    if (variant === "danger") {
      s.color = C.danger;
      s.borderColor = C.danger;
    }
    return s;
  }
  var inputStyle = {
    width: "100%", padding: "0.4rem 0.5rem", fontSize: "0.8rem", fontFamily: "inherit",
    background: C.bg, color: C.fg, border: "1px solid " + C.border,
  };
  var sub = {
    fontSize: "0.62rem", fontWeight: 700, letterSpacing: "0.06em", textTransform: "uppercase",
    color: C.muted, marginBottom: "0.2rem",
  };

  // ── mesh graph ────────────────────────────────────────────────────
  function shorten(x1, y1, x2, y2, pad) {
    var dx = x2 - x1, dy = y2 - y1, len = Math.sqrt(dx * dx + dy * dy) || 1;
    return {
      x1: x1 + (dx / len) * pad, y1: y1 + (dy / len) * pad,
      x2: x2 - (dx / len) * pad, y2: y2 - (dy / len) * pad,
    };
  }

  function MeshGraph(props) {
    var mesh = props.mesh;
    var caps = mesh.capabilities || [];

    // Node set: every plugin ratd knows, plus any plugin named in a capability.
    var nodeMap = {};
    (mesh.plugins || []).forEach(function (p) {
      nodeMap[p.name] = { name: p.name, healthy: !!p.healthy, known: true };
    });
    caps.forEach(function (c) {
      if (c.provider && !nodeMap[c.provider]) {
        nodeMap[c.provider] = { name: c.provider, healthy: false, known: false };
      }
      (c.consumers || []).forEach(function (cn) {
        if (!nodeMap[cn]) nodeMap[cn] = { name: cn, healthy: false, known: false };
      });
    });
    var nodes = Object.keys(nodeMap).map(function (k) { return nodeMap[k]; });

    if (nodes.length === 0) {
      return h("div", { style: { color: C.muted, fontSize: "0.82rem", padding: "1.5rem" } },
        "No plugins registered yet.");
    }

    var W = 640, Hh = 380, cx = W / 2, cy = Hh / 2;
    var R = Math.min(W, Hh) / 2 - 72;
    var pos = {};
    nodes.forEach(function (n, i) {
      var ang = -Math.PI / 2 + (2 * Math.PI * i) / nodes.length;
      pos[n.name] = nodes.length === 1
        ? { x: cx, y: cy }
        : { x: cx + R * Math.cos(ang), y: cy + R * Math.sin(ang) };
    });

    var edges = [];
    caps.forEach(function (c) {
      (c.consumers || []).forEach(function (cn) {
        if (pos[cn] && pos[c.provider] && cn !== c.provider) {
          edges.push({ from: cn, to: c.provider, label: c.name });
        }
      });
    });

    return h("svg", {
        viewBox: "0 0 " + W + " " + Hh,
        style: { width: "100%", height: "auto", maxHeight: "420px", display: "block" },
      },
      h("defs", null,
        h("marker", {
          id: "ic-arrow", viewBox: "0 0 10 10", refX: 8, refY: 5,
          markerWidth: 7, markerHeight: 7, orient: "auto-start-reverse",
        }, h("path", { d: "M0,0 L10,5 L0,10 z", fill: LINE }))),
      // edges
      edges.map(function (e, i) {
        var a = pos[e.from], b = pos[e.to];
        var s = shorten(a.x, a.y, b.x, b.y, 40);
        var mx = (s.x1 + s.x2) / 2, my = (s.y1 + s.y2) / 2;
        return h("g", { key: "e" + i },
          h("line", {
            x1: s.x1, y1: s.y1, x2: s.x2, y2: s.y2,
            stroke: LINE, strokeWidth: 1.5, markerEnd: "url(#ic-arrow)",
          }),
          h("text", {
            x: mx, y: my - 4, fontSize: 9, fill: LINE, textAnchor: "middle",
          }, e.label));
      }),
      // nodes
      nodes.map(function (n, i) {
        var p = pos[n.name];
        var dot = n.known ? (n.healthy ? GREEN : RED) : GREY;
        var isSelf = n.name === "interconnect";
        return h("g", { key: "n" + n.name },
          h("rect", {
            x: p.x - 62, y: p.y - 20, width: 124, height: 40, rx: 2,
            fill: "#16181d",
            stroke: isSelf ? GREEN : "#3a3d44", strokeWidth: isSelf ? 2 : 1,
          }),
          h("circle", { cx: p.x + 50, cy: p.y - 9, r: 4, fill: dot }),
          h("text", {
            x: p.x, y: p.y + 4, fontSize: 11, fontWeight: 600,
            fill: "#e5e5e5", textAnchor: "middle",
          }, n.name));
      })
    );
  }

  // ── invoke tester / capability card ──────────────────────────────
  function CapabilityCard(props) {
    var c = props.cap;
    var openState = React.useState(false);
    var open = openState[0], setOpen = openState[1];
    var payloadState = React.useState("{}");
    var payload = payloadState[0], setPayload = payloadState[1];
    var resState = React.useState(null);
    var res = resState[0], setRes = resState[1];
    var busyState = React.useState(false);
    var busy = busyState[0], setBusy = busyState[1];

    function invoke() {
      var body = { capability: c.name };
      if (c.method !== "GET") {
        try {
          body.payload = JSON.parse(payload || "{}");
        } catch (e) {
          setRes({ error: "payload is not valid JSON" });
          return;
        }
      }
      setBusy(true);
      setRes(null);
      req("POST", "/invoke", body)
        .then(function (r) { setRes(r); })
        .catch(function (e) { setRes({ error: String((e && e.message) || e) }); })
        .then(function () { setBusy(false); });
    }

    return h("div", { style: { ...cardStyle, marginBottom: "0.5rem" } },
      h("div", { style: { display: "flex", justifyContent: "space-between", alignItems: "baseline", gap: "0.5rem", flexWrap: "wrap" } },
        h("div", null,
          h("span", { style: { fontWeight: 700, fontSize: "0.86rem" } }, c.name),
          h("span", { style: { fontSize: "0.72rem", color: C.muted, marginLeft: "0.5rem" } },
            c.method + " → " + c.provider + c.path)),
        h("div", { style: { display: "flex", gap: "0.35rem" } },
          h("button", { onClick: function () { setOpen(!open); }, style: btn() },
            open ? "Close" : "▶ Invoke"),
          h("button", { onClick: function () { props.onDelete(c.name); }, style: btn("danger") }, "✕"))),
      c.description
        ? h("div", { style: { fontSize: "0.76rem", color: C.muted, marginTop: "0.3rem" } }, c.description)
        : null,
      c.consumers && c.consumers.length
        ? h("div", { style: { fontSize: "0.68rem", color: C.muted, marginTop: "0.25rem" } },
            "consumed by: " + c.consumers.join(", "))
        : null,
      open
        ? h("div", { style: { marginTop: "0.6rem", borderTop: "1px solid " + C.border, paddingTop: "0.6rem" } },
            c.method !== "GET"
              ? h("div", { style: { marginBottom: "0.5rem" } },
                  h("div", { style: sub }, "Payload (JSON)"),
                  h("textarea", {
                    value: payload, onChange: function (e) { setPayload(e.target.value); },
                    rows: 3, style: { ...inputStyle, resize: "vertical", fontFamily: "monospace" },
                  }))
              : null,
            h("button", { onClick: invoke, disabled: busy, style: btn("primary") },
              busy ? "Invoking…" : "Send through the broker"),
            res
              ? h("div", { style: { marginTop: "0.6rem" } },
                  res.error
                    ? h("div", { style: { color: C.danger, fontSize: "0.78rem" } }, res.error)
                    : h("div", null,
                        h("div", { style: { fontSize: "0.7rem", color: C.muted, marginBottom: "0.2rem" } },
                          "via " + res.provider + " · HTTP " + res.status),
                        h("pre", {
                          style: {
                            fontSize: "0.74rem", fontFamily: "monospace", whiteSpace: "pre-wrap",
                            background: C.bg, border: "1px solid " + C.border, padding: "0.5rem",
                            margin: 0, maxHeight: "220px", overflow: "auto",
                          },
                        }, pretty(res.body))))
              : null)
        : null
    );
  }

  function pretty(v) {
    try {
      return JSON.stringify(v, null, 2);
    } catch (e) {
      return String(v);
    }
  }

  // ── register form ─────────────────────────────────────────────────
  function RegisterForm(props) {
    var f = React.useState({ method: "POST", path: "/" });
    var form = f[0], setForm = f[1];
    var errState = React.useState("");
    var err = errState[0], setErr = errState[1];
    function set(k, v) { setForm({ ...form, [k]: v }); }

    function submit() {
      setErr("");
      if (!form.name || !form.provider) {
        setErr("Name and provider are required.");
        return;
      }
      req("POST", "/register", {
        name: form.name, provider: form.provider, method: form.method,
        path: form.path || "/", description: form.description || "",
        consumers: (form.consumers || "").split(",").map(function (s) { return s.trim(); })
          .filter(function (s) { return s; }),
      })
        .then(function () {
          setForm({ method: "POST", path: "/" });
          props.onRegistered();
        })
        .catch(function (e) { setErr(String((e && e.message) || e)); });
    }

    var field = function (label, key, ph) {
      return h("div", { style: { flex: 1, minWidth: "9rem" } },
        h("div", { style: sub }, label),
        h("input", {
          type: "text", value: form[key] || "", placeholder: ph,
          onChange: function (e) { set(key, e.target.value); }, style: inputStyle,
        }));
    };

    return h("div", { style: { ...cardStyle, marginBottom: "1rem" } },
      h("div", { style: { fontWeight: 700, fontSize: "0.82rem", marginBottom: "0.6rem" } },
        "Register a capability"),
      h("div", { style: { display: "flex", gap: "0.6rem", flexWrap: "wrap", marginBottom: "0.5rem" } },
        field("Name", "name", "e.g. data.analyze"),
        field("Provider plugin", "provider", "e.g. event-notifier")),
      h("div", { style: { display: "flex", gap: "0.6rem", flexWrap: "wrap", marginBottom: "0.5rem" } },
        h("div", { style: { minWidth: "7rem" } },
          h("div", { style: sub }, "Method"),
          h("select", {
            value: form.method, onChange: function (e) { set("method", e.target.value); },
            style: inputStyle,
          }, ["GET", "POST", "PUT", "DELETE"].map(function (m) {
            return h("option", { key: m, value: m }, m);
          }))),
        field("Path", "path", "/events"),
        field("Consumers (comma-sep)", "consumers", "interconnect, ai")),
      field("Description", "description", "what this capability does"),
      err ? h("div", { style: { color: C.danger, fontSize: "0.76rem", marginTop: "0.4rem" } }, err) : null,
      h("div", { style: { marginTop: "0.6rem" } },
        h("button", { onClick: submit, style: btn("primary") }, "Register"))
    );
  }

  // ── app ───────────────────────────────────────────────────────────
  function InterconnectApp() {
    var st = React.useState({ loading: true });
    var state = st[0], setState = st[1];
    var showFormState = React.useState(false);
    var showForm = showFormState[0], setShowForm = showFormState[1];

    function load() {
      setState(function (s) { return { loading: true, mesh: s.mesh }; });
      req("GET", "/mesh")
        .then(function (m) { setState({ loading: false, mesh: m }); })
        .catch(function (e) { setState({ loading: false, error: String((e && e.message) || e) }); });
    }
    React.useEffect(function () { load(); }, []);

    function removeCap(name) {
      req("DELETE", "/capabilities/" + encodeURIComponent(name)).then(load).catch(load);
    }

    var mesh = state.mesh || { plugins: [], capabilities: [] };
    var caps = mesh.capabilities || [];

    return h("div", { style: { maxWidth: "60rem", margin: "0 auto" } },
      h("div", { style: { display: "flex", alignItems: "baseline", justifyContent: "space-between", flexWrap: "wrap", gap: "0.5rem" } },
        h("h1", { style: { fontWeight: 800, fontSize: "1.4rem" } }, "🔌 Plugin Mesh"),
        h("div", { style: { display: "flex", gap: "0.4rem" } },
          h("button", { onClick: function () { setShowForm(!showForm); }, style: btn() },
            showForm ? "Hide form" : "+ Register capability"),
          h("button", { onClick: load, style: btn() }, "⟳ Refresh"))),
      h("p", { style: { fontSize: "0.8rem", color: C.muted, margin: "0.2rem 0 1rem" } },
        "Every plugin and how capabilities wire them together. Invoke a capability " +
          "by name — the broker routes it to a healthy provider."),

      state.error
        ? h("div", {
            style: {
              color: C.danger, fontSize: "0.8rem", border: "1px solid " + C.danger,
              padding: "0.5rem 0.65rem", marginBottom: "1rem",
            },
          }, state.error)
        : null,

      showForm ? h(RegisterForm, { onRegistered: function () { setShowForm(false); load(); } }) : null,

      // the graph
      h("div", { style: { ...cardStyle, marginBottom: "1rem" } },
        h("div", { style: { ...sub, marginBottom: "0.5rem" } },
          "Mesh · " + (mesh.plugins || []).length + " plugins · " + caps.length + " capabilities"),
        state.loading && !state.mesh
          ? h("div", { style: { color: C.muted, fontSize: "0.82rem", padding: "1.5rem" } }, "Loading…")
          : h(MeshGraph, { mesh: mesh })),

      // capabilities
      h("div", { style: { ...sub, margin: "0 0 0.5rem" } }, "Capabilities"),
      caps.length === 0
        ? h("div", { style: { color: C.muted, fontSize: "0.82rem" } },
            "No capabilities registered yet — use “+ Register capability”.")
        : caps.map(function (c) {
            return h(CapabilityCard, { key: c.name, cap: c, onDelete: removeCap });
          })
    );
  }

  window.__RAT_REGISTER_PLUGIN("interconnect", {
    navItems: [{ label: "Plugin Mesh", icon: "network", href: "/x/interconnect", priority: 30 }],
    routes: [{ path: "/x/interconnect", component: InterconnectApp }],
  });
  console.info("[interconnect] plugin mesh registered with the portal");
})();
