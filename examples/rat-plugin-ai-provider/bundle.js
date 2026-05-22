/*
 * rat-plugin-ai-provider — portal UI bundle (Layer 3): the AI Provider page.
 *
 * Build-free: uses the portal's window.React and window.__RAT_REGISTER_PLUGIN.
 * Shows the plugin's effective configuration, the capabilities it exposes for
 * other plugins, and a prompt tester that doubles as a connection check.
 *
 * Configuration itself is edited in the portal's Plugins page (the config
 * editor renders a form from this plugin's config_schema_json).
 */
(function () {
  "use strict";

  var React = window.React;
  if (!React || typeof window.__RAT_REGISTER_PLUGIN !== "function") {
    console.warn("[ai-provider] RAT plugin host not available — skipping");
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
    danger: "hsl(var(--destructive, 0 62% 35%))",
    warn: "hsl(var(--warning, 38 92% 50%))",
  };

  function apiBase() {
    var s = document.querySelector('script[src*="/plugins/ai-provider/ui/bundle.js"]');
    if (s && s.src && s.src.indexOf("/api/v1/") !== -1) {
      return s.src.slice(0, s.src.indexOf("/api/v1/"));
    }
    return window.location.origin;
  }
  var API = apiBase() + "/api/v1/x/ai-provider";

  function req(method, path, body) {
    var opts = { method: method, headers: {} };
    if (body !== undefined) {
      opts.headers["Content-Type"] = "application/json";
      opts.body = JSON.stringify(body);
    }
    return fetch(API + path, opts).then(function (res) {
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
  var cardStyle = { border: "1px solid " + C.border, background: C.card, padding: "0.9rem", marginBottom: "1rem" };
  function btn(variant) {
    var s = {
      fontSize: "0.76rem", fontWeight: 600, padding: "0.34rem 0.8rem", cursor: "pointer",
      fontFamily: "inherit", border: "1px solid " + C.border, background: "transparent", color: C.fg,
    };
    if (variant === "primary") {
      s.background = C.primary;
      s.color = "hsl(var(--primary-foreground, 0 0% 2%))";
      s.borderColor = C.primary;
    }
    return s;
  }
  var inputStyle = {
    width: "100%", padding: "0.45rem 0.55rem", fontSize: "0.82rem", fontFamily: "inherit",
    background: C.bg, color: C.fg, border: "1px solid " + C.border, boxSizing: "border-box",
  };
  var sub = {
    fontSize: "0.63rem", fontWeight: 700, letterSpacing: "0.06em", textTransform: "uppercase",
    color: C.muted, marginBottom: "0.4rem",
  };

  function Row(props) {
    return h("div", {
        style: {
          display: "flex", justifyContent: "space-between", gap: "1rem",
          padding: "0.32rem 0", borderBottom: "1px solid " + C.border, fontSize: "0.82rem",
        },
      },
      h("span", { style: { color: C.muted } }, props.label),
      h("span", { style: { fontFamily: "monospace", textAlign: "right", wordBreak: "break-all" } },
        props.children));
  }

  // ── app ───────────────────────────────────────────────────────────
  function AIProviderApp() {
    var cfgState = React.useState(null);
    var cfg = cfgState[0], setCfg = cfgState[1];
    var cfgErrState = React.useState("");
    var cfgErr = cfgErrState[0], setCfgErr = cfgErrState[1];

    var promptState = React.useState("Say hello in one short sentence.");
    var prompt = promptState[0], setPrompt = promptState[1];
    var sysState = React.useState("");
    var sys = sysState[0], setSys = sysState[1];
    var resState = React.useState(null);
    var res = resState[0], setRes = resState[1];
    var busyState = React.useState(false);
    var busy = busyState[0], setBusy = busyState[1];

    function loadConfig() {
      setCfgErr("");
      req("GET", "/config")
        .then(function (c) { setCfg(c); })
        .catch(function (e) { setCfgErr(String((e && e.message) || e)); });
    }
    React.useEffect(function () { loadConfig(); }, []);

    function runComplete() {
      if (!prompt.trim()) return;
      setBusy(true);
      setRes(null);
      req("POST", "/complete", { prompt: prompt, system: sys })
        .then(function (r) { setRes(r); })
        .catch(function (e) { setRes({ error: String((e && e.message) || e) }); })
        .then(function () { setBusy(false); });
    }

    var configured = cfg && cfg.base_url && cfg.model;

    return h("div", { style: { maxWidth: "52rem", margin: "0 auto" } },
      h("h1", { style: { fontWeight: 800, fontSize: "1.4rem" } }, "✨ AI Provider"),
      h("p", { style: { fontSize: "0.82rem", color: C.muted, margin: "0.2rem 0 1.1rem" } },
        "A configurable, reusable LLM service. Other plugins call its " +
          "/complete and /chat endpoints — directly or by capability through the plugin mesh."),

      // ── effective config ──
      h("div", { style: cardStyle },
        h("div", { style: { display: "flex", justifyContent: "space-between", alignItems: "center" } },
          h("div", { style: sub }, "Effective configuration"),
          h("button", { onClick: loadConfig, style: btn() }, "⟳ Reload")),
        cfgErr
          ? h("div", { style: { color: C.danger, fontSize: "0.8rem" } }, cfgErr)
          : !cfg
            ? h("div", { style: { color: C.muted, fontSize: "0.82rem" } }, "Loading…")
            : h("div", null,
                h(Row, { label: "API base URL" }, cfg.base_url || "— not set —"),
                h(Row, { label: "Model" }, cfg.model || "— not set —"),
                h(Row, { label: "API key" }, cfg.api_key_set ? "set" : "not set"),
                h(Row, { label: "Default system prompt" }, cfg.system_prompt || "(none)")),
        h("div", { style: { fontSize: "0.74rem", color: C.muted, marginTop: "0.6rem", lineHeight: 1.5 } },
          "Edit these in the portal's ", h("strong", null, "Plugins"),
          " page — expand ", h("strong", null, "ai-provider"),
          " and change its settings. The plugin polls ratd and picks up changes within ~15s " +
            "(hit Reload to check).")),

      !configured && cfg
        ? h("div", {
            style: {
              ...cardStyle, borderColor: C.warn, color: C.warn, fontSize: "0.8rem",
            },
          }, "⚠ Not fully configured — set the API base URL and model in the plugin settings.")
        : null,

      // ── capabilities ──
      h("div", { style: cardStyle },
        h("div", { style: sub }, "Reusable by other plugins"),
        h("div", { style: { fontSize: "0.82rem", lineHeight: 1.6 } },
          "Other AI extensions reuse this provider — no LLM code or keys of their own. ",
          "It registers two capabilities with the ", h("strong", null, "interconnect"), " plugin:"),
        h("ul", { style: { fontSize: "0.8rem", margin: "0.5rem 0 0", paddingLeft: "1.1rem", lineHeight: 1.7 } },
          h("li", null, h("code", null, "ai.complete"), " — one-shot completion (POST /complete)"),
          h("li", null, h("code", null, "ai.chat"), " — raw multi-message chat (POST /chat)"))),

      // ── tester ──
      h("div", { style: cardStyle },
        h("div", { style: sub }, "Try it"),
        h("div", { style: { marginBottom: "0.55rem" } },
          h("div", { style: { fontSize: "0.72rem", color: C.muted, marginBottom: "0.2rem" } },
            "System prompt (optional)"),
          h("input", {
            type: "text", value: sys, placeholder: "leave empty to use the default",
            onChange: function (e) { setSys(e.target.value); }, style: inputStyle,
          })),
        h("div", { style: { marginBottom: "0.55rem" } },
          h("div", { style: { fontSize: "0.72rem", color: C.muted, marginBottom: "0.2rem" } }, "Prompt"),
          h("textarea", {
            value: prompt, onChange: function (e) { setPrompt(e.target.value); },
            rows: 3, style: { ...inputStyle, resize: "vertical" },
          })),
        h("button", { onClick: runComplete, disabled: busy, style: btn("primary") },
          busy ? "Calling the model…" : "Complete"),
        res
          ? h("div", { style: { marginTop: "0.7rem" } },
              res.error
                ? h("div", { style: { color: C.danger, fontSize: "0.82rem" } }, "✗ " + res.error)
                : h("div", null,
                    h("div", { style: { fontSize: "0.7rem", color: C.muted, marginBottom: "0.25rem" } },
                      "model: " + (res.model || "?")),
                    h("div", {
                      style: {
                        fontSize: "0.85rem", whiteSpace: "pre-wrap", background: C.bg,
                        border: "1px solid " + C.border, padding: "0.6rem", lineHeight: 1.5,
                      },
                    }, res.text || "(empty response)")))
          : null)
    );
  }

  window.__RAT_REGISTER_PLUGIN("ai-provider", {
    navItems: [{ label: "AI Provider", icon: "sparkles", href: "/x/ai-provider", priority: 40 }],
    routes: [{ path: "/x/ai-provider", component: AIProviderApp }],
  });
  console.info("[ai-provider] registered with the portal");
})();
