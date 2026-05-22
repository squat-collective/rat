/*
 * rat-plugin-ai — portal UI bundle (Layer 3): the AI Data Navigator chat page.
 *
 * Build-free: the portal exposes React on window.React and a registration hook
 * on window.__RAT_REGISTER_PLUGIN. Registers an /x/ai chat page + a nav item.
 *
 * Includes a small markdown renderer (code blocks, lists, inline code/bold) and
 * a chart renderer (bar + line) for charts the assistant produces.
 */
(function () {
  "use strict";

  var React = window.React;
  if (!React || typeof window.__RAT_REGISTER_PLUGIN !== "function") {
    console.warn("[ai] RAT plugin host not available — skipping");
    return;
  }
  var h = React.createElement;

  function apiBase() {
    var s = document.querySelector('script[src*="/plugins/ai/ui/bundle.js"]');
    if (s && s.src && s.src.indexOf("/api/v1/") !== -1) {
      return s.src.slice(0, s.src.indexOf("/api/v1/"));
    }
    return window.location.origin;
  }
  var CHAT_URL = apiBase() + "/api/v1/x/ai/chat";

  function newSessionId() {
    if (window.crypto && crypto.randomUUID) return "sess-" + crypto.randomUUID();
    return "sess-" + Math.random().toString(36).slice(2);
  }

  function truncate(s, n) {
    s = s || "";
    return s.length > n ? s.slice(0, n) + "…" : s;
  }

  function fmtNum(v) {
    if (typeof v !== "number") return String(v);
    return Number.isInteger(v) ? String(v) : v.toFixed(2);
  }

  // ── Minimal markdown rendering ───────────────────────────────────
  var codeStyle = {
    fontFamily: "monospace", fontSize: "0.8rem",
    background: "rgba(255,255,255,0.08)", padding: "0.05rem 0.25rem",
  };
  var preStyle = {
    fontFamily: "monospace", fontSize: "0.78rem", whiteSpace: "pre-wrap",
    background: "rgba(255,255,255,0.06)", padding: "0.6rem", overflowX: "auto",
    margin: "0.4rem 0",
  };

  function renderInline(text) {
    var parts = String(text).split(/(`[^`]+`|\*\*[^*]+\*\*)/g);
    return parts.map(function (p, i) {
      if (p.length > 1 && p[0] === "`" && p[p.length - 1] === "`") {
        return h("code", { key: i, style: codeStyle }, p.slice(1, -1));
      }
      if (p.length > 3 && p.slice(0, 2) === "**" && p.slice(-2) === "**") {
        return h("strong", { key: i }, p.slice(2, -2));
      }
      return p;
    });
  }

  function renderMarkdown(text) {
    // Strip image markdown — small models sometimes fabricate base64 image
    // blobs; real charts are rendered separately from the response's charts[].
    var clean = String(text || "").replace(/!\[[^\]]*\]\([^)]*\)/g, "");
    var lines = clean.split("\n");
    var blocks = [];
    var i = 0;
    while (i < lines.length) {
      var line = lines[i];
      if (line.trim().slice(0, 3) === "```") {
        var code = [];
        i++;
        while (i < lines.length && lines[i].trim().slice(0, 3) !== "```") {
          code.push(lines[i]); i++;
        }
        i++;
        blocks.push(h("pre", { key: blocks.length, style: preStyle }, code.join("\n")));
      } else if (/^\s*[-*]\s+/.test(line)) {
        var bul = [];
        while (i < lines.length && /^\s*[-*]\s+/.test(lines[i])) {
          bul.push(lines[i].replace(/^\s*[-*]\s+/, "")); i++;
        }
        blocks.push(h("ul", { key: blocks.length, style: { margin: "0.3rem 0", paddingLeft: "1.1rem" } },
          bul.map(function (it, j) { return h("li", { key: j }, renderInline(it)); })));
      } else if (/^\s*\d+\.\s+/.test(line)) {
        var num = [];
        while (i < lines.length && /^\s*\d+\.\s+/.test(lines[i])) {
          num.push(lines[i].replace(/^\s*\d+\.\s+/, "")); i++;
        }
        blocks.push(h("ol", { key: blocks.length, style: { margin: "0.3rem 0", paddingLeft: "1.3rem" } },
          num.map(function (it, j) { return h("li", { key: j }, renderInline(it)); })));
      } else if (line.trim() === "") {
        i++;
      } else {
        var para = [];
        while (i < lines.length && lines[i].trim() !== "" &&
               lines[i].trim().slice(0, 3) !== "```" &&
               !/^\s*[-*]\s+/.test(lines[i]) && !/^\s*\d+\.\s+/.test(lines[i])) {
          para.push(lines[i]); i++;
        }
        blocks.push(h("div", {
          key: blocks.length,
          style: { whiteSpace: "pre-wrap", marginBottom: "0.35rem" },
        }, renderInline(para.join("\n"))));
      }
    }
    return blocks;
  }

  // ── Charts ───────────────────────────────────────────────────────
  function Chart(props) {
    var spec = props.spec;
    var values = spec.values || [];
    var labels = spec.labels || [];
    return h("div", { className: "brutal-card", style: { padding: "0.75rem", marginTop: "0.5rem" } },
      h("div", { style: { fontWeight: "bold", fontSize: "0.78rem", marginBottom: "0.5rem" } },
        spec.title || "Chart"),
      spec.type === "line" ? lineChart(labels, values) : barChart(labels, values)
    );
  }

  function barChart(labels, values) {
    var max = Math.max.apply(null, values.concat([0]));
    return h("div", { style: { display: "flex", flexDirection: "column", gap: "0.25rem" } },
      values.map(function (v, i) {
        var pct = max > 0 ? (v / max) * 100 : 0;
        return h("div", {
            key: i,
            style: { display: "flex", alignItems: "center", gap: "0.5rem", fontSize: "0.72rem" },
          },
          h("div", {
            style: {
              width: "32%", textAlign: "right", opacity: 0.7,
              overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap",
            },
          }, String(labels[i])),
          h("div", { style: { flex: 1, background: "rgba(255,255,255,0.06)" } },
            h("div", {
              style: {
                width: pct + "%", minWidth: "2px", height: "1.05rem",
                background: "var(--primary, #4ade80)",
              },
            })),
          h("div", { style: { width: "3.6rem", fontFamily: "monospace" } }, fmtNum(v))
        );
      })
    );
  }

  function lineChart(labels, values) {
    var W = 480, H = 150, pad = 26;
    var n = values.length;
    var max = Math.max.apply(null, values.concat([0]));
    var min = Math.min.apply(null, values.concat([0]));
    var span = (max - min) || 1;
    function px(i) { return pad + (n <= 1 ? (W - 2 * pad) / 2 : (i / (n - 1)) * (W - 2 * pad)); }
    function py(v) { return H - pad - ((v - min) / span) * (H - 2 * pad); }
    var pts = values.map(function (v, i) { return px(i) + "," + py(v); }).join(" ");
    return h("svg", { viewBox: "0 0 " + W + " " + H, style: { width: "100%", height: "auto" } },
      h("polyline", { points: pts, fill: "none", stroke: "var(--primary, #4ade80)", strokeWidth: 2 }),
      values.map(function (v, i) {
        return h("circle", { key: i, cx: px(i), cy: py(v), r: 3, fill: "var(--primary, #4ade80)" });
      }),
      h("text", { x: 2, y: pad - 4, fontSize: 9, fill: "currentColor", opacity: 0.55 }, fmtNum(max)),
      h("text", { x: pad, y: H - 8, fontSize: 9, fill: "currentColor", opacity: 0.55 },
        truncate(String(labels[0] || ""), 16)),
      n > 1 ? h("text", {
        x: W - pad, y: H - 8, fontSize: 9, fill: "currentColor", opacity: 0.55, textAnchor: "end",
      }, truncate(String(labels[n - 1] || ""), 16)) : null
    );
  }

  // ── Chat ─────────────────────────────────────────────────────────
  function Message(props) {
    var m = props.m;
    var isUser = m.role === "user";
    var isErr = m.role === "error";
    return h("div", {
        className: "brutal-card",
        style: {
          padding: "0.75rem", alignSelf: isUser ? "flex-end" : "flex-start",
          maxWidth: "88%", borderColor: isErr ? "#f87171" : undefined,
        },
      },
      h("div", {
        style: {
          fontSize: "0.6rem", fontWeight: "bold", letterSpacing: "0.08em",
          textTransform: "uppercase", opacity: 0.55, marginBottom: "0.35rem",
        },
      }, isUser ? "you" : isErr ? "error" : "assistant"),
      (m.steps && m.steps.length)
        ? h("div", { style: { fontSize: "0.68rem", opacity: 0.55, marginBottom: "0.45rem", fontFamily: "monospace" } },
            m.steps.map(function (s, j) {
              return h("div", { key: j }, "🔧 " + s.tool + "(" + truncate(s.args, 88) + ")");
            }))
        : null,
      isUser
        ? h("div", { style: { whiteSpace: "pre-wrap", fontSize: "0.85rem" } }, m.content)
        : h("div", { style: { fontSize: "0.85rem", lineHeight: 1.5 } }, renderMarkdown(m.content)),
      (m.charts && m.charts.length)
        ? m.charts.map(function (c, j) { return h(Chart, { key: j, spec: c }); })
        : null
    );
  }

  var EXAMPLES = [
    "What tables do I have?",
    "How many rows are in default.bronze.fr_orders?",
    "Bar chart of amount by name in default.bronze.sd_orders",
  ];

  function AIChatPage() {
    var msgsState = React.useState([]);
    var messages = msgsState[0], setMessages = msgsState[1];
    var inputState = React.useState("");
    var input = inputState[0], setInput = inputState[1];
    var busyState = React.useState(false);
    var busy = busyState[0], setBusy = busyState[1];
    var sessionRef = React.useRef(newSessionId());
    var endRef = React.useRef(null);

    React.useEffect(function () {
      if (endRef.current) endRef.current.scrollIntoView({ behavior: "smooth" });
    }, [messages, busy]);

    function send(preset) {
      var text = ((typeof preset === "string" && preset) || input).trim();
      if (!text || busy) return;
      var next = messages.concat([{ role: "user", content: text }]);
      setMessages(next);
      setInput("");
      setBusy(true);
      fetch(CHAT_URL, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ session_id: sessionRef.current, message: text }),
      })
        .then(function (r) { return r.json(); })
        .then(function (data) {
          if (data.session_id) sessionRef.current = data.session_id;
          var role = data.error ? "error" : "assistant";
          setMessages(next.concat([{
            role: role,
            content: data.error || data.reply || "(no reply)",
            steps: data.steps,
            charts: data.charts,
          }]));
        })
        .catch(function (err) {
          setMessages(next.concat([{ role: "error", content: String(err) }]));
        })
        .then(function () { setBusy(false); });
    }

    function newChat() {
      sessionRef.current = newSessionId();
      setMessages([]);
      setInput("");
    }

    return h("div", {
        style: {
          display: "flex", flexDirection: "column",
          height: "calc(100vh - 9rem)", maxWidth: "52rem", margin: "0 auto",
        },
      },
      h("div", { style: { display: "flex", alignItems: "baseline", justifyContent: "space-between" } },
        h("h1", { style: { fontWeight: "bold" } }, "✨ AI Data Navigator"),
        messages.length
          ? h("button", {
              onClick: function () { newChat(); },
              className: "brutal-card",
              style: { fontSize: "0.7rem", padding: "0.25rem 0.6rem", cursor: "pointer" },
            }, "New chat")
          : null
      ),
      h("p", { style: { fontSize: "0.8rem", opacity: 0.6, margin: "0.2rem 0 0.75rem" } },
        "Ask about your data — the assistant inspects schemas, runs queries, and " +
          "can draw charts. The conversation is continuable."),
      h("div", {
          style: {
            flex: 1, overflowY: "auto", display: "flex", flexDirection: "column",
            gap: "0.6rem", paddingRight: "0.25rem",
          },
        },
        messages.length === 0
          ? h("div", { style: { padding: "0.5rem", display: "flex", flexWrap: "wrap", gap: "0.4rem" } },
              EXAMPLES.map(function (ex, i) {
                return h("button", {
                  key: i,
                  onClick: function () { send(ex); },
                  className: "brutal-card",
                  style: { fontSize: "0.75rem", padding: "0.35rem 0.6rem", cursor: "pointer", opacity: 0.85 },
                }, ex);
              }))
          : messages.map(function (m, i) { return h(Message, { key: i, m: m }); }),
        busy
          ? h("div", { style: { opacity: 0.6, fontSize: "0.85rem", padding: "0.5rem" } }, "thinking…")
          : null,
        h("div", { ref: endRef })
      ),
      h("div", { style: { display: "flex", gap: "0.5rem", marginTop: "0.75rem" } },
        h("textarea", {
          value: input,
          onChange: function (e) { setInput(e.target.value); },
          onKeyDown: function (e) {
            if (e.key === "Enter" && !e.shiftKey) { e.preventDefault(); send(); }
          },
          rows: 2,
          placeholder: "Ask about your data…  (Enter to send, Shift+Enter for newline)",
          className: "brutal-card",
          style: {
            flex: 1, padding: "0.5rem", resize: "none", fontFamily: "inherit",
            fontSize: "0.85rem", background: "transparent", color: "inherit",
          },
        }),
        h("button", {
          onClick: function () { send(); },
          disabled: busy,
          className: "brutal-card",
          style: {
            padding: "0 1.25rem", fontWeight: "bold",
            cursor: busy ? "default" : "pointer", opacity: busy ? 0.5 : 1,
          },
        }, "Send")
      )
    );
  }

  window.__RAT_REGISTER_PLUGIN("ai", {
    navItems: [{ label: "AI Assistant", icon: "sparkles", href: "/x/ai", priority: 10 }],
    routes: [{ path: "/x/ai", component: AIChatPage }],
  });
  console.info("[ai] data navigator registered with the portal");
})();
