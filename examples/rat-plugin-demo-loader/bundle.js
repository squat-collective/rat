/*
 * rat-plugin-demo-loader — portal UI bundle (Layer 3).
 *
 * Build-free: uses the portal's window.React. Adds a "Demos" sidebar entry
 * at /x/demo-loader where the user picks a sample-data demo and installs it
 * with one click. The plugin's L2 then creates the namespace, pipelines,
 * quality tests, writes the SQL files and submits the initial bronze runs.
 */
(function () {
  "use strict";

  var React = window.React;
  if (!React || typeof window.__RAT_REGISTER_PLUGIN !== "function") {
    console.warn("[demo-loader] RAT plugin host not available — skipping");
    return;
  }
  var h = React.createElement;

  var C = {
    border: "hsl(var(--border, 0 0% 16%))",
    fg: "hsl(var(--foreground, 0 0% 90%))",
    muted: "hsl(var(--muted-foreground, 0 0% 50%))",
    card: "hsl(var(--card, 0 0% 7%))",
    surface: "hsl(var(--secondary, 0 0% 12%))",
    primary: "hsl(var(--primary, 142 72% 45%))",
    bg: "hsl(var(--background, 0 0% 4%))",
    danger: "hsl(var(--destructive, 0 62% 45%))",
  };

  function apiBase() {
    var s = document.querySelector('script[src*="/plugins/demo-loader/ui/bundle.js"]');
    if (s && s.src && s.src.indexOf("/api/v1/") !== -1) {
      return s.src.slice(0, s.src.indexOf("/api/v1/"));
    }
    return window.location.origin;
  }
  var API = apiBase();

  function req(method, path, body) {
    var opts = { method: method, headers: {} };
    if (body !== undefined) {
      opts.headers["Content-Type"] = "application/json";
      opts.body = JSON.stringify(body);
    }
    return fetch(API + path, opts).then(function (r) {
      return r.text().then(function (t) {
        var d = null;
        try { d = t ? JSON.parse(t) : null; } catch (e) { d = { error: t }; }
        if (!d) d = {};
        if (!r.ok && !d.error) d.error = "request failed: " + r.status;
        return d;
      });
    });
  }

  var btn = {
    fontSize: "0.74rem", fontWeight: 600, padding: "0.35rem 0.8rem", cursor: "pointer",
    fontFamily: "inherit", border: "1px solid " + C.border, background: "transparent", color: C.fg,
  };
  var btnPrimary = Object.assign({}, btn, {
    background: C.primary,
    color: "hsl(var(--primary-foreground, 0 0% 2%))",
    borderColor: C.primary,
  });
  var cardStyle = { border: "1px solid " + C.border, background: C.card, padding: "0.9rem", marginBottom: "0.8rem" };
  var sub = {
    fontSize: "0.62rem", fontWeight: 700, color: C.muted,
    textTransform: "uppercase", letterSpacing: "0.06em",
  };

  function DemoCard(props) {
    var d = props.demo;
    var stateS = React.useState({ status: "idle" });
    var state = stateS[0], setState = stateS[1];
    var nsS = React.useState(d.namespace);
    var ns = nsS[0], setNs = nsS[1];

    function install() {
      var target = (ns || "").trim() || d.namespace;
      setState({ status: "installing" });
      req("POST", "/api/v1/x/demo-loader/install", { demo_id: d.id, namespace: target })
        .then(function (res) {
          if (typeof window.__RAT_INVALIDATE === "function") {
            window.__RAT_INVALIDATE();
          }
          setState({ status: "done", result: res });
        })
        .catch(function (e) {
          setState({ status: "error", error: String((e && e.message) || e) });
        });
    }

    var installed = state.status === "done" && state.result && (!state.result.errors || state.result.errors.length === 0);
    var partial = state.status === "done" && state.result && state.result.errors && state.result.errors.length > 0;

    return h("div", { style: cardStyle },
      h("div", { style: { display: "flex", justifyContent: "space-between", alignItems: "baseline", gap: "0.5rem", flexWrap: "wrap" } },
        h("div", null,
          h("div", { style: { fontWeight: 800, fontSize: "1rem" } }, d.name),
          h("div", { style: { fontSize: "0.7rem", color: C.muted, marginTop: "0.15rem" } },
            d.pipeline_count, " pipelines · ", d.test_count, " quality tests")),
        h("div", { style: { display: "flex", gap: "0.4rem", alignItems: "center", flexWrap: "wrap" } },
          h("label", { style: { fontSize: "0.6rem", color: C.muted, textTransform: "uppercase", letterSpacing: "0.05em" } }, "namespace"),
          h("input", {
            type: "text",
            value: ns,
            placeholder: d.namespace,
            onChange: function (e) { setNs(e.target.value); },
            disabled: state.status === "installing",
            style: {
              fontSize: "0.76rem", fontFamily: "monospace",
              padding: "0.2rem 0.4rem", width: "8.5rem",
              background: C.bg, color: C.fg, border: "1px solid " + C.border,
            },
          }),
          state.status === "installing"
            ? h("span", { style: { fontSize: "0.78rem", color: C.muted } }, "Installing…")
            : null,
          installed
            ? h("span", { style: { fontSize: "0.78rem", color: C.primary } }, "✓ Installed")
            : null,
          h("button", {
            onClick: install,
            disabled: state.status === "installing",
            style: state.status === "done" ? btn : btnPrimary,
          }, state.status === "done" ? "Reinstall" : "Install"))),
      h("p", { style: { fontSize: "0.82rem", color: C.muted, margin: "0.6rem 0 0", lineHeight: 1.5 } }, d.description),
      state.status === "error"
        ? h("div", { style: { color: C.danger, fontSize: "0.78rem", marginTop: "0.5rem" } }, "✗ " + state.error)
        : null,
      state.status === "done" && state.result
        ? h("div", { style: { marginTop: "0.7rem", paddingTop: "0.7rem", borderTop: "1px solid " + C.border } },
            h("div", { style: sub }, partial ? "Installed with errors" : "Installed"),
            h("div", { style: { fontSize: "0.74rem", color: C.muted, marginTop: "0.4rem", lineHeight: 1.5 } },
              "namespace ",
              h("a", {
                href: "/pipelines?namespace=" + encodeURIComponent(state.result.namespace || d.namespace),
                style: { color: C.primary, textDecoration: "underline" },
              }, state.result.namespace || d.namespace),
              ": ", state.result.steps ? state.result.steps.length : 0, " steps completed"),
            partial
              ? h("div", { style: { color: C.danger, fontSize: "0.74rem", marginTop: "0.4rem" } },
                  "errors: ",
                  h("ul", { style: { margin: "0.2rem 0 0 1rem" } },
                    (state.result.errors || []).map(function (e, i) {
                      return h("li", { key: i }, e);
                    })))
              : null)
        : null);
  }

  function DemoLoaderApp() {
    var st = React.useState({ loading: true });
    var state = st[0], setState = st[1];

    function load() {
      setState({ loading: true });
      req("GET", "/api/v1/x/demo-loader/demos")
        .then(function (data) {
          setState({ loading: false, demos: Array.isArray(data) ? data : [] });
        })
        .catch(function (e) {
          setState({ loading: false, error: String((e && e.message) || e) });
        });
    }
    React.useEffect(load, []);

    return h("div", { style: { maxWidth: "52rem", margin: "0 auto" } },
      h("h1", { style: { fontWeight: 800, fontSize: "1.4rem" } }, "✨ Demos"),
      h("p", { style: { fontSize: "0.82rem", color: C.muted, margin: "0.2rem 0 1.2rem" } },
        "One-click sample-data demos. Each install creates the namespace, " +
          "writes the pipeline SQL, creates the quality tests and submits the " +
          "bronze runs — silver and gold are yours to trigger from the pipelines page."),
      state.loading
        ? h("div", { style: { color: C.muted, fontSize: "0.82rem" } }, "Loading demos…")
        : state.error
          ? h("div", { style: { color: C.danger, fontSize: "0.82rem" } }, "✗ " + state.error)
          : (state.demos || []).length === 0
            ? h("div", { style: { color: C.muted, fontSize: "0.82rem" } }, "No demos available.")
            : (state.demos || []).map(function (d) { return h(DemoCard, { key: d.id, demo: d }); }));
  }

  window.__RAT_REGISTER_PLUGIN("demo-loader", {
    navItems: [{ label: "Demos", icon: "sparkles", href: "/x/demo-loader", priority: 80 }],
    routes: [{ path: "/x/demo-loader", component: DemoLoaderApp }],
  });
  console.info("[demo-loader] registered with the portal");
})();
