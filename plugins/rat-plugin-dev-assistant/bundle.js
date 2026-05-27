/*
 * rat-plugin-dev-assistant — portal UI bundle (Layer 3).
 *
 * Build-free: uses the portal's window.React. Registers a panel into the
 * core "pipeline-editor-sidebar" slot. The panel chats with the dev
 * assistant — which brokers to the ai-provider — sending the current file
 * (and optionally a data sample) as context, and can apply AI-generated
 * code straight into the editor via the slot's onApply callback.
 */
(function () {
  "use strict";

  var React = window.React;
  if (!React || typeof window.__RAT_REGISTER_PLUGIN !== "function") {
    console.warn("[dev-assistant] RAT plugin host not available — skipping");
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
    var s = document.querySelector('script[src*="/plugins/dev-assistant/ui/bundle.js"]');
    if (s && s.src && s.src.indexOf("/api/v1/") !== -1) {
      return s.src.slice(0, s.src.indexOf("/api/v1/"));
    }
    return window.location.origin;
  }
  var API = apiBase();

  function req(path, body) {
    return fetch(API + path, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body),
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

  // ── markdown rendering ────────────────────────────────────────────
  function splitFences(text) {
    var segs = [];
    var re = /```(\w*)\n?([\s\S]*?)```/g;
    var last = 0, m;
    while ((m = re.exec(text)) !== null) {
      if (m.index > last) segs.push({ type: "prose", text: text.slice(last, m.index) });
      segs.push({ type: "code", lang: m[1] || "", text: m[2].replace(/\n$/, "") });
      last = m.index + m[0].length;
    }
    if (last < text.length) segs.push({ type: "prose", text: text.slice(last) });
    return segs;
  }

  function inline(s, key) {
    var parts = [];
    var re = /(\*\*[^*]+\*\*|`[^`]+`)/g;
    var last = 0, m, i = 0;
    while ((m = re.exec(s)) !== null) {
      if (m.index > last) parts.push(s.slice(last, m.index));
      var tok = m[0];
      if (tok.indexOf("**") === 0) {
        parts.push(h("strong", { key: key + "b" + i++ }, tok.slice(2, -2)));
      } else {
        parts.push(h("code", {
          key: key + "c" + i++,
          style: { fontFamily: "monospace", background: C.surface, padding: "0 0.2rem", fontSize: "0.74rem" },
        }, tok.slice(1, -1)));
      }
      last = m.index + m[0].length;
    }
    if (last < s.length) parts.push(s.slice(last));
    return parts;
  }

  function renderProse(text, key) {
    var out = [];
    var listBuf = [];
    function flushList() {
      if (listBuf.length) {
        out.push(h("ul", { key: key + "ul" + out.length, style: { margin: "0.25rem 0", paddingLeft: "1.1rem" } },
          listBuf.map(function (li, idx) { return h("li", { key: idx }, inline(li, key + "li" + idx)); })));
        listBuf = [];
      }
    }
    text.split("\n").forEach(function (ln, idx) {
      var t = ln.trim();
      if (/^[-*]\s+/.test(t)) { listBuf.push(t.replace(/^[-*]\s+/, "")); return; }
      flushList();
      if (t === "") return;
      if (/^#{1,6}\s/.test(t)) {
        out.push(h("div", { key: key + "h" + idx, style: { fontWeight: 700, margin: "0.4rem 0 0.15rem" } },
          inline(t.replace(/^#+\s/, ""), key + "hi" + idx)));
        return;
      }
      out.push(h("p", { key: key + "p" + idx, style: { margin: "0.3rem 0", lineHeight: 1.5 } },
        inline(t, key + "pi" + idx)));
    });
    flushList();
    return out;
  }

  function CodeBlock(props) {
    var st = React.useState(false);
    var applied = st[0], setApplied = st[1];
    function apply() {
      if (!props.onApply) return;
      props.onApply(props.text);
      setApplied(true);
      setTimeout(function () { setApplied(false); }, 2500);
    }
    return h("div", { style: { margin: "0.5rem 0", border: "1px solid " + C.border } },
      h("div", {
        style: {
          display: "flex", justifyContent: "space-between", alignItems: "center",
          padding: "0.2rem 0.45rem", background: C.surface,
          fontSize: "0.62rem", color: C.muted, textTransform: "uppercase", letterSpacing: "0.05em",
        },
      },
        h("span", null, props.lang || "code"),
        props.onApply
          ? h("button", {
              onClick: apply,
              style: {
                fontSize: "0.64rem", fontWeight: 700, cursor: "pointer", fontFamily: "inherit",
                border: "1px solid " + (applied ? C.primary : C.border),
                background: applied ? C.primary : "transparent",
                color: applied ? "hsl(var(--primary-foreground, 0 0% 2%))" : C.fg,
                padding: "0.1rem 0.4rem",
              },
            }, applied ? "✓ Applied" : "Apply to editor")
          : null),
      h("pre", {
        style: {
          margin: 0, padding: "0.5rem", overflow: "auto", maxHeight: "260px",
          fontSize: "0.72rem", fontFamily: "monospace", whiteSpace: "pre", background: C.bg,
        },
      }, props.text));
  }

  function MarkdownMessage(props) {
    return h("div", null, splitFences(props.text).map(function (seg, i) {
      if (seg.type === "code") {
        return h(CodeBlock, { key: i, lang: seg.lang, text: seg.text, onApply: props.onApply });
      }
      return h("div", { key: i }, renderProse(seg.text, "s" + i));
    }));
  }

  // ── preview / data sample ─────────────────────────────────────────
  function formatSample(d) {
    if (!d || !d.columns) return "";
    if (d.error) return "(preview failed: " + d.error + ")";
    var cols = d.columns.map(function (c) { return c.name + " (" + (c.type || "?") + ")"; }).join(", ");
    var rows = (d.rows || []).slice(0, 12).map(function (r) { return JSON.stringify(r); }).join("\n");
    return "columns: " + cols + (rows ? "\nrows:\n" + rows : "");
  }

  // ── the panel ─────────────────────────────────────────────────────
  function DevAssistantPanel(props) {
    var openS = React.useState(true);
    var open = openS[0], setOpen = openS[1];
    var msgsS = React.useState([]);
    var msgs = msgsS[0], setMsgs = msgsS[1];
    var inputS = React.useState("");
    var input = inputS[0], setInput = inputS[1];
    var busyS = React.useState(false);
    var busy = busyS[0], setBusy = busyS[1];
    var sampleS = React.useState(true);
    var includeSample = sampleS[0], setIncludeSample = sampleS[1];

    var scrollRef = React.useRef(null);
    React.useEffect(function () {
      if (scrollRef.current) scrollRef.current.scrollTop = scrollRef.current.scrollHeight;
    }, [msgs, busy]);

    function fetchSample() {
      var p = props.pipeline;
      if (!p || !p.namespace) return Promise.resolve("");
      return fetch(API + "/api/v1/pipelines/" + p.namespace + "/" + p.layer + "/" + p.name + "/preview", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ limit: 15, code: props.getContent ? props.getContent() : undefined }),
      })
        .then(function (r) { return r.ok ? r.json() : null; })
        .then(function (d) { return d ? formatSample(d) : ""; })
        .catch(function () { return ""; });
    }

    function send(text) {
      text = (text || "").trim();
      if (!text || busy) return;
      var next = msgs.concat([{ role: "user", content: text }]);
      setMsgs(next);
      setInput("");
      setBusy(true);

      var p = props.pipeline || {};
      var ctx = {
        pipeline: { namespace: p.namespace, layer: p.layer, name: p.name },
        language: props.language || "",
        file_content: props.getContent ? props.getContent() : "",
      };
      var samplePromise = includeSample ? fetchSample() : Promise.resolve("");
      samplePromise
        .then(function (sample) {
          ctx.data_sample = sample;
          return req("/api/v1/x/dev-assistant/chat", { messages: next, context: ctx });
        })
        .then(function (res) {
          var reply = res && res.error ? "⚠ " + res.error : (res && res.reply) || "(no response)";
          setMsgs(next.concat([{ role: "assistant", content: reply }]));
        })
        .catch(function (e) {
          setMsgs(next.concat([{ role: "assistant", content: "⚠ " + String((e && e.message) || e) }]));
        })
        .then(function () { setBusy(false); });
    }

    // Collapsed — a thin bar.
    if (!open) {
      return h("div", {
          style: {
            width: "2.4rem", borderLeft: "1px solid " + C.border, background: C.card,
            display: "flex", flexDirection: "column", alignItems: "center", paddingTop: "0.5rem",
          },
        },
        h("button", {
          onClick: function () { setOpen(true); },
          title: "Open Dev Assistant",
          style: { background: "transparent", border: "none", cursor: "pointer", fontSize: "1.1rem" },
        }, "🤖"));
    }

    var btn = {
      fontSize: "0.72rem", fontWeight: 600, padding: "0.3rem 0.6rem", cursor: "pointer",
      fontFamily: "inherit", border: "1px solid " + C.border, background: "transparent", color: C.fg,
    };
    var examples = [
      "Explain what this pipeline does",
      "Suggest improvements to this pipeline",
      "How do I make this incremental?",
    ];

    return h("div", {
        style: {
          width: "23rem", borderLeft: "1px solid " + C.border, background: C.card,
          display: "flex", flexDirection: "column", height: "100%", minHeight: 0,
        },
      },
      // header
      h("div", {
        style: {
          display: "flex", justifyContent: "space-between", alignItems: "center",
          padding: "0.4rem 0.6rem", borderBottom: "1px solid " + C.border,
        },
      },
        h("span", { style: { fontWeight: 800, fontSize: "0.82rem" } }, "🤖 Dev Assistant"),
        h("div", { style: { display: "flex", gap: "0.3rem" } },
          msgs.length
            ? h("button", { onClick: function () { setMsgs([]); }, style: { ...btn, padding: "0.15rem 0.4rem" }, title: "Clear" }, "Clear")
            : null,
          h("button", { onClick: function () { setOpen(false); }, style: { ...btn, padding: "0.15rem 0.4rem" }, title: "Collapse" }, "»"))),

      // messages
      h("div", { ref: scrollRef, style: { flex: 1, overflowY: "auto", padding: "0.6rem", fontSize: "0.8rem" } },
        msgs.length === 0
          ? h("div", { style: { color: C.muted } },
              h("p", { style: { lineHeight: 1.5, margin: "0 0 0.6rem" } },
                "Ask me to write, explain or fix a pipeline. I can see your current file" +
                  (includeSample ? " and a sample of its data" : "") + "."),
              examples.map(function (ex, i) {
                return h("button", {
                  key: i,
                  onClick: function () { send(ex); },
                  style: { ...btn, display: "block", width: "100%", textAlign: "left", marginBottom: "0.3rem" },
                }, ex);
              }))
          : msgs.map(function (m, i) {
              var isUser = m.role === "user";
              return h("div", {
                  key: i,
                  style: {
                    margin: "0.5rem 0", padding: "0.45rem 0.6rem",
                    background: isUser ? C.surface : "transparent",
                    border: isUser ? "none" : "1px solid " + C.border,
                  },
                },
                h("div", { style: { fontSize: "0.6rem", fontWeight: 700, color: C.muted, textTransform: "uppercase", marginBottom: "0.2rem" } },
                  isUser ? "You" : "Assistant"),
                isUser
                  ? h("div", { style: { whiteSpace: "pre-wrap", lineHeight: 1.5 } }, m.content)
                  : h(MarkdownMessage, { text: m.content, onApply: props.onApply }));
            }),
        busy ? h("div", { style: { color: C.muted, fontSize: "0.76rem", padding: "0.3rem" } }, "Thinking…") : null),

      // controls
      h("div", { style: { borderTop: "1px solid " + C.border, padding: "0.5rem" } },
        h("div", { style: { display: "flex", justifyContent: "space-between", alignItems: "center", marginBottom: "0.4rem" } },
          h("button", { onClick: function () { send("Explain this file."); }, disabled: busy, style: btn }, "Explain file"),
          h("label", { style: { fontSize: "0.68rem", color: C.muted, display: "flex", alignItems: "center", gap: "0.25rem", cursor: "pointer" } },
            h("input", {
              type: "checkbox", checked: includeSample,
              onChange: function (e) { setIncludeSample(e.target.checked); },
            }),
            "data sample")),
        h("textarea", {
          value: input,
          onChange: function (e) { setInput(e.target.value); },
          onKeyDown: function (e) {
            if (e.key === "Enter" && (e.ctrlKey || e.metaKey)) { e.preventDefault(); send(input); }
          },
          rows: 3,
          placeholder: "Describe what you want…  (Ctrl+Enter to send)",
          style: {
            width: "100%", boxSizing: "border-box", resize: "vertical", fontFamily: "inherit",
            fontSize: "0.78rem", padding: "0.4rem", background: C.bg, color: C.fg,
            border: "1px solid " + C.border,
          },
        }),
        h("button", {
          onClick: function () { send(input); },
          disabled: busy || !input.trim(),
          style: {
            ...btn, width: "100%", marginTop: "0.35rem",
            background: busy || !input.trim() ? "transparent" : C.primary,
            color: busy || !input.trim() ? C.muted : "hsl(var(--primary-foreground, 0 0% 2%))",
            borderColor: busy || !input.trim() ? C.border : C.primary,
          },
        }, busy ? "Working…" : "Send")));
  }

  window.__RAT_REGISTER_PLUGIN("dev-assistant", {
    slots: { "pipeline-editor-sidebar": [DevAssistantPanel] },
  });
  console.info("[dev-assistant] editor panel registered with the portal");
})();
