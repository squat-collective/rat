/*
 * rat-plugin-secrets — portal UI bundle (Layer 3).
 *
 * A dedicated /x/secrets page: list, add, update, delete encrypted
 * secrets. Values are write-once-by-name — there is no "reveal" UI
 * because the API never returns plaintexts to a list call. To rotate
 * a secret, save a new value under the same name.
 *
 * Build-free: no JSX, no bundler. Uses window.React + the host
 * portal's __RAT_REGISTER_PLUGIN hook.
 */
(function () {
  "use strict";

  var React = window.React;
  if (!React || typeof window.__RAT_REGISTER_PLUGIN !== "function") {
    console.warn("[secrets] RAT plugin host not available — skipping");
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
    danger: "hsl(var(--destructive, 0 62% 35%))",
    warn: "hsl(var(--warning, 38 92% 50%))",
  };

  function apiBase() {
    var s = document.querySelector('script[src*="/plugins/secrets/ui/bundle.js"]');
    if (s && s.src && s.src.indexOf("/api/v1/") !== -1) {
      return s.src.slice(0, s.src.indexOf("/api/v1/"));
    }
    return window.location.origin;
  }
  var API = apiBase() + "/api/v1/x/secrets";

  function req(method, path, body) {
    var opts = { method: method, headers: {} };
    if (body !== undefined) {
      opts.headers["Content-Type"] = "application/json";
      opts.body = JSON.stringify(body);
    }
    return fetch(API + path, opts).then(function (res) {
      return res.text().then(function (t) {
        var d = null;
        try { d = t ? JSON.parse(t) : null; } catch (e) { d = { error: t }; }
        if (!res.ok) throw new Error((d && d.error) || ("HTTP " + res.status));
        return d;
      });
    });
  }

  function inputStyle() {
    return {
      width: "100%", padding: 6, background: C.bg, color: C.fg,
      border: "1px solid " + C.border, fontFamily: "inherit", fontSize: 13,
      boxSizing: "border-box",
    };
  }
  function btnStyle(bg, fg) {
    return {
      padding: "4px 10px", background: bg, color: fg,
      border: "1px solid " + C.border, fontSize: 11,
      cursor: "pointer", letterSpacing: 0.5,
    };
  }
  function labeled(label, child) {
    return h("div", { style: { marginTop: 10 } },
      h("div", { style: { color: C.muted, fontSize: 10, textTransform: "uppercase", letterSpacing: 1, marginBottom: 4 } }, label),
      child,
    );
  }
  function relativeTime(iso) {
    if (!iso) return "";
    var t = new Date(iso).getTime();
    var diff = (Date.now() - t) / 1000;
    if (diff < 60) return Math.round(diff) + "s";
    if (diff < 3600) return Math.round(diff / 60) + "m";
    if (diff < 86400) return Math.round(diff / 3600) + "h";
    return Math.round(diff / 86400) + "d";
  }

  // ── Form ───────────────────────────────────────────────────────

  function SecretForm(props) {
    var st1 = useState(props.initial || { name: "", value: "", description: "" });
    var s = st1[0], setS = st1[1];
    var st2 = useState(false), busy = st2[0], setBusy = st2[1];
    var st3 = useState(null), err = st3[0], setErr = st3[1];
    var isEdit = !!props.initial;

    function save() {
      setBusy(true); setErr(null);
      req("POST", "/secrets", s).then(function () {
        setBusy(false);
        props.onSaved();
      }).catch(function (e) {
        setErr((e && e.message) || String(e));
        setBusy(false);
      });
    }

    return h("div", {
      style: { border: "1px solid " + C.border, padding: 16, background: C.card },
    },
      h("div", { style: { fontWeight: 700, fontSize: 13, marginBottom: 8 } },
        isEdit ? "Update secret" : "New secret"),
      labeled("Name (lowercase, snake_case)", h("input", {
        value: s.name, onChange: function (e) { setS(Object.assign({}, s, { name: e.target.value.trim() })); },
        disabled: isEdit, placeholder: "e.g. pg_prod",
        style: Object.assign({}, inputStyle(), { fontFamily: "monospace" }),
      })),
      labeled("Value (will be encrypted at rest)", h("input", {
        type: "password", value: s.value, autoComplete: "new-password",
        onChange: function (e) { setS(Object.assign({}, s, { value: e.target.value })); },
        placeholder: isEdit ? "leave empty to keep current — type to rotate" : "the secret content",
        style: Object.assign({}, inputStyle(), { fontFamily: "monospace" }),
      })),
      labeled("Description (optional)", h("input", {
        value: s.description || "", onChange: function (e) { setS(Object.assign({}, s, { description: e.target.value })); },
        placeholder: "Where this secret is used / what it grants",
        style: inputStyle(),
      })),
      err && h("div", {
        style: { marginTop: 8, color: C.danger, fontSize: 11,
                 padding: 6, background: "rgba(239,68,68,0.08)",
                 border: "1px solid " + C.danger },
      }, "error: " + err),
      h("div", { style: { display: "flex", justifyContent: "flex-end", gap: 6, marginTop: 12 } },
        h("button", { type: "button", onClick: props.onCancel, style: btnStyle("transparent", C.fg) }, "Cancel"),
        h("button", {
          type: "button", onClick: save, disabled: busy || !s.name || !s.value,
          style: btnStyle(busy || !s.name || !s.value ? C.muted : C.primary, C.bg),
        }, busy ? "saving…" : (isEdit ? "Save" : "Create")),
      ),
    );
  }

  // ── Main ───────────────────────────────────────────────────────

  function SecretsApp() {
    var st1 = useState([]), secrets = st1[0], setSecrets = st1[1];
    var st2 = useState(false), loading = st2[0], setLoading = st2[1];
    var st3 = useState(null), err = st3[0], setErr = st3[1];
    var st4 = useState(null), editing = st4[0], setEditing = st4[1];

    var refresh = useCallback(function () {
      setLoading(true);
      req("GET", "/secrets").then(function (d) {
        setSecrets((d && d.secrets) || []);
        setLoading(false);
      }).catch(function (e) {
        setErr(e.message); setLoading(false);
      });
    }, []);

    useEffect(refresh, [refresh]);

    function remove(name) {
      if (!window.confirm("Delete secret \"" + name + "\"? Plugins depending on it will start failing immediately.")) return;
      req("DELETE", "/secrets/" + encodeURIComponent(name)).then(refresh).catch(function (e) { setErr(e.message); });
    }

    return h("div", {
      style: { padding: 20, color: C.fg, background: C.bg, minHeight: "calc(100vh - 60px)" },
    },
      h("div", { style: { display: "flex", alignItems: "center", gap: 12, marginBottom: 16 } },
        h("h1", { style: { margin: 0, fontSize: 16, letterSpacing: 1, fontWeight: 700 } }, "SECRETS"),
        h("span", { style: { color: C.muted, fontSize: 12 } },
          secrets.length + " stored — encrypted at rest"),
        h("div", { style: { marginLeft: "auto" } },
          !editing && h("button", {
            onClick: function () { setEditing({}); setErr(null); },
            style: btnStyle(C.primary, C.bg),
          }, "+ New secret"),
        ),
      ),

      err && h("div", {
        style: { padding: 10, background: "rgba(239,68,68,0.10)",
                 border: "1px solid " + C.danger, color: C.danger,
                 fontSize: 12, marginBottom: 12 },
      }, "error: " + err),

      editing
        ? h(SecretForm, {
            initial: editing.name ? editing : null,
            onSaved: function () { setEditing(null); refresh(); },
            onCancel: function () { setEditing(null); setErr(null); },
          })
        : h("div", { style: { display: "grid", gap: 8 } },
            loading && h("div", { style: { color: C.muted, fontSize: 12 } }, "loading…"),
            !loading && secrets.length === 0 && h("div", {
              style: { color: C.muted, fontSize: 13, padding: 16, border: "1px dashed " + C.border },
            },
              "No secrets yet. Click ", h("strong", null, "+ New secret"),
              " to add one. Other plugins can then ask the interconnect broker for ",
              h("code", { style: { fontFamily: "monospace" } }, "secrets.get"),
              " with the name."),
            secrets.map(function (s) {
              return h("div", {
                key: s.name,
                style: { padding: 10, border: "1px solid " + C.border,
                         background: C.card, display: "flex", alignItems: "center", gap: 12 },
              },
                h("div", { style: { flex: 1, minWidth: 0 } },
                  h("div", { style: { display: "flex", alignItems: "baseline", gap: 8 } },
                    h("span", { style: { fontFamily: "monospace", fontWeight: 700, fontSize: 13 } }, s.name),
                    h("span", { style: { color: C.muted, fontSize: 10 } },
                      "updated " + relativeTime(s.updated_at)),
                  ),
                  s.description && h("div", { style: { color: C.muted, fontSize: 11, marginTop: 4 } }, s.description),
                ),
                h("div", { style: { display: "flex", gap: 6 } },
                  h("button", {
                    onClick: function () { setEditing({ name: s.name, value: "", description: s.description || "" }); setErr(null); },
                    style: btnStyle("transparent", C.fg),
                  }, "rotate"),
                  h("button", {
                    onClick: function () { remove(s.name); },
                    style: btnStyle("transparent", C.danger),
                  }, "delete"),
                ),
              );
            }),
          ),
    );
  }

  window.__RAT_REGISTER_PLUGIN("secrets", {
    navItems: [{ label: "Secrets", icon: "key", href: "/x/secrets", priority: 12 }],
    routes: [{ path: "/x/secrets", component: SecretsApp }],
  });
  console.info("[secrets] registered with the portal");
})();
