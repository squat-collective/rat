/*
 * rat-plugin-ai — portal UI bundle (Layer 3): the AI Data Navigator chat page.
 *
 * Build-free: the portal exposes React on window.React and a registration hook
 * on window.__RAT_REGISTER_PLUGIN. Registers an /x/ai chat page + a nav item.
 *
 * Markdown renderer + chart rendering: graphs are drawn with the charts
 * plugin's own renderer (window.__RAT_CHARTS) when available, with a build-free
 * fallback, and can be pinned onto a dashboard.
 */
(function () {
  "use strict";

  var React = window.React;
  if (!React || typeof window.__RAT_REGISTER_PLUGIN !== "function") {
    console.warn("[ai] RAT plugin host not available — skipping");
    return;
  }
  var h = React.createElement;
  // Portal --primary as a literal: the portal's CSS vars are bare HSL triplets
  // (hsl(var(--primary))), and SVG fill/stroke attributes don't resolve CSS
  // vars anyway — so a hex literal is the only thing that works everywhere.
  var ACCENT = "#4ade80";
  // Portal theme colours wrapped in hsl() — the portal's CSS vars are bare HSL
  // triplets, so they must be wrapped or they render as nothing.
  var BORDER = "hsl(var(--border, 0 0% 16%))";
  var CARD = "hsl(var(--card, 0 0% 7%))";

  function apiBase() {
    var s = document.querySelector('script[src*="/plugins/ai/ui/bundle.js"]');
    if (s && s.src && s.src.indexOf("/api/v1/") !== -1) {
      return s.src.slice(0, s.src.indexOf("/api/v1/"));
    }
    return window.location.origin;
  }
  var CHAT_URL = apiBase() + "/api/v1/x/ai/chat";
  var CHARTS_API = apiBase() + "/api/v1/x/charts";

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
    // Split on inline code, bold, and [label](url) links.
    var parts = String(text).split(/(`[^`]+`|\*\*[^*]+\*\*|\[[^\]]+\]\([^)]+\))/g);
    return parts.map(function (p, i) {
      if (p.length > 1 && p[0] === "`" && p[p.length - 1] === "`") {
        return h("code", { key: i, style: codeStyle }, p.slice(1, -1));
      }
      if (p.length > 3 && p.slice(0, 2) === "**" && p.slice(-2) === "**") {
        return h("strong", { key: i }, p.slice(2, -2));
      }
      var link = /^\[([^\]]+)\]\(([^)]+)\)$/.exec(p);
      if (link) {
        return h("a", {
          key: i,
          href: link[2],
          style: { color: ACCENT, textDecoration: "underline" },
        }, link[1]);
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
  // Graphs are drawn with the charts ("Dashboards") plugin's own Recharts
  // renderer, exposed on window.__RAT_CHARTS. If that plugin is not installed a
  // simple build-free fallback (first series only) is used instead.
  var SLICE_COLORS = ["#4ade80", "#22d3ee", "#a78bfa", "#fbbf24", "#f472b6", "#60a5fa"];
  var PALETTE_LEAD = {
    rat: "#4ade80", vivid: "#6366f1", ocean: "#38bdf8", sunset: "#fb7185", mono: "#e5e5e5",
  };

  function toNumber(v) {
    if (typeof v === "number") return v;
    var n = Number(v);
    return Number.isFinite(n) ? n : 0;
  }

  function optionColor(opts) {
    opts = opts || {};
    if (opts.colors && opts.colors[0]) return opts.colors[0];
    return PALETTE_LEAD[opts.palette] || ACCENT;
  }

  // Chart renders one graph the assistant produced — through the charts
  // plugin's renderer when available — with a button to pin it onto a board.
  function Chart(props) {
    var spec = props.spec;
    var pinState = React.useState(false);
    var pinning = pinState[0], setPinning = pinState[1];
    var bridge = window.__RAT_CHARTS;
    var body;
    if (bridge && bridge.ChartView && spec.rows) {
      body = h(bridge.ChartView, {
        chart: {
          type: spec.type, x_column: spec.x_column,
          y_columns: spec.y_columns, options: spec.options,
        },
        rows: spec.rows,
        height: 260,
      });
    } else {
      body = buildFreeChart(spec);
    }
    return h("div", { className: "brutal-card", style: { padding: "0.75rem", marginTop: "0.5rem" } },
      h("div", {
          style: {
            display: "flex", justifyContent: "space-between", alignItems: "center",
            gap: "0.5rem", marginBottom: "0.5rem",
          },
        },
        h("div", { style: { fontWeight: "bold", fontSize: "0.78rem" } }, spec.title || "Chart"),
        h("button", {
          onClick: function () { setPinning(true); },
          style: {
            fontSize: "0.66rem", fontWeight: 600, padding: "0.15rem 0.45rem",
            border: "1px solid " + BORDER, background: "transparent", color: "inherit",
            cursor: "pointer", fontFamily: "inherit", whiteSpace: "nowrap",
          },
        }, "📌 Pin")
      ),
      body,
      pinning ? h(PinModal, { spec: spec, onClose: function () { setPinning(false); } }) : null
    );
  }

  // buildFreeChart is the fallback used when the charts plugin is absent — it
  // derives the first series from the rows and draws it build-free.
  function buildFreeChart(spec) {
    var rows = spec.rows || [];
    var x = spec.x_column;
    var y = (spec.y_columns || [])[0];
    if (!x || !y || !rows.length) {
      return h("div", { style: { fontSize: "0.72rem", opacity: 0.6, padding: "0.4rem 0" } },
        "no data to chart");
    }
    var labels = rows.map(function (r) { return r[x]; });
    var values = rows.map(function (r) { return toNumber(r[y]); });
    var color = optionColor(spec.options);
    if (spec.type === "pie") return pieChart(labels, values);
    if (spec.type === "line") return lineChart(labels, values, color);
    if (spec.type === "area") return areaChart(labels, values, color);
    if (spec.type === "radar") {
      return h("div", { style: { fontSize: "0.72rem", opacity: 0.6, padding: "0.4rem 0" } },
        "radar charts need the Dashboards plugin");
    }
    return barChart(labels, values, color);
  }

  // PinModal attaches a chat-generated chart onto a dashboard as a live chart
  // component (its SQL re-runs on the dashboard).
  function PinModal(props) {
    var spec = props.spec;
    var st = React.useState({ loading: true });
    var state = st[0], setState = st[1];
    var nameState = React.useState("");
    var name = nameState[0], setName = nameState[1];
    var msgState = React.useState("");
    var msg = msgState[0], setMsg = msgState[1];

    React.useEffect(function () {
      fetch(CHARTS_API + "/dashboards")
        .then(function (r) { return r.json(); })
        .then(function (d) { setState({ loading: false, list: Array.isArray(d) ? d : [] }); })
        .catch(function (e) { setState({ loading: false, error: String(e) }); });
    }, []);

    function chartComponent() {
      return {
        type: "chart",
        props: {
          title: spec.title, chart_type: spec.type, sql: spec.sql,
          x_column: spec.x_column, y_columns: spec.y_columns, options: spec.options,
        },
      };
    }
    function pinTo(dashId) {
      setMsg("Pinning…");
      fetch(CHARTS_API + "/dashboards/" + dashId + "/components", {
        method: "POST", headers: { "Content-Type": "application/json" },
        body: JSON.stringify(chartComponent()),
      })
        .then(function (r) { if (!r.ok) throw new Error("HTTP " + r.status); return r.json(); })
        .then(function () { setMsg("✓ Pinned to the dashboard."); })
        .catch(function (e) { setMsg("Could not pin: " + e); });
    }
    function createAndPin() {
      if (!name.trim()) return;
      setMsg("Creating…");
      fetch(CHARTS_API + "/dashboards", {
        method: "POST", headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ title: name.trim() }),
      })
        .then(function (r) { if (!r.ok) throw new Error("HTTP " + r.status); return r.json(); })
        .then(function (d) { pinTo(d.id); })
        .catch(function (e) { setMsg("Could not create: " + e); });
    }

    var rowBtn = {
      textAlign: "left", padding: "0.45rem 0.6rem", border: "1px solid " + BORDER,
      background: "transparent", color: "inherit", cursor: "pointer",
      fontFamily: "inherit", fontSize: "0.82rem",
    };
    return h("div", {
        onClick: props.onClose,
        style: {
          position: "fixed", inset: 0, background: "rgba(0,0,0,0.72)", zIndex: 70,
          display: "flex", alignItems: "flex-start", justifyContent: "center",
          padding: "4rem 1rem", overflowY: "auto",
        },
      },
      h("div", {
          onClick: function (e) { e.stopPropagation(); },
          style: {
            background: CARD, border: "2px solid " + BORDER,
            padding: "1.1rem", width: "100%", maxWidth: "26rem",
          },
        },
        h("div", { style: { fontWeight: "bold", marginBottom: "0.75rem" } },
          "📌 Pin chart to a dashboard"),
        state.loading ? h("div", { style: { opacity: 0.6, fontSize: "0.8rem" } }, "Loading…") : null,
        state.error
          ? h("div", { style: { color: "#f87171", fontSize: "0.78rem" } }, state.error)
          : null,
        state.list && state.list.length
          ? h("div", {
                style: { display: "flex", flexDirection: "column", gap: "0.3rem", marginBottom: "0.75rem" },
              },
              state.list.map(function (d) {
                return h("button", { key: d.id, onClick: function () { pinTo(d.id); }, style: rowBtn },
                  d.title);
              }))
          : state.list
            ? h("div", { style: { opacity: 0.6, fontSize: "0.78rem", marginBottom: "0.75rem" } },
                "No dashboards yet — create one below.")
            : null,
        h("div", { style: { display: "flex", gap: "0.4rem" } },
          h("input", {
            value: name,
            onChange: function (e) { setName(e.target.value); },
            placeholder: "New dashboard name…",
            style: {
              flex: 1, padding: "0.4rem", background: "transparent", color: "inherit",
              border: "1px solid " + BORDER, fontFamily: "inherit", fontSize: "0.82rem",
            },
          }),
          h("button", {
            onClick: createAndPin,
            style: {
              padding: "0 0.7rem", fontWeight: "bold", cursor: "pointer", border: "1px solid " + ACCENT,
              background: ACCENT, color: "#0a0a0a", fontFamily: "inherit", fontSize: "0.78rem",
            },
          }, "Create & pin")
        ),
        msg ? h("div", { style: { marginTop: "0.7rem", fontSize: "0.8rem" } }, msg) : null,
        h("div", { style: { marginTop: "0.85rem", textAlign: "right" } },
          h("button", {
            onClick: props.onClose,
            style: {
              fontSize: "0.75rem", padding: "0.25rem 0.7rem", border: "1px solid " + BORDER,
              background: "transparent", color: "inherit", cursor: "pointer", fontFamily: "inherit",
            },
          }, "Close"))
      )
    );
  }

  function barChart(labels, values, color) {
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
              style: { width: pct + "%", minWidth: "2px", height: "1.05rem", background: color },
            })),
          h("div", { style: { width: "3.6rem", fontFamily: "monospace" } }, fmtNum(v))
        );
      })
    );
  }

  // linePoints computes the SVG geometry shared by the line and area previews.
  function linePoints(values, W, H, pad) {
    var n = values.length;
    var max = Math.max.apply(null, values.concat([0]));
    var min = Math.min.apply(null, values.concat([0]));
    var span = (max - min) || 1;
    function px(i) { return pad + (n <= 1 ? (W - 2 * pad) / 2 : (i / (n - 1)) * (W - 2 * pad)); }
    function py(v) { return H - pad - ((v - min) / span) * (H - 2 * pad); }
    return { px: px, py: py, max: max };
  }

  function axisText(max, labels, n, W, H, pad) {
    return [
      h("text", { key: "m", x: 2, y: pad - 4, fontSize: 9, fill: "currentColor", opacity: 0.55 },
        fmtNum(max)),
      h("text", { key: "l0", x: pad, y: H - 8, fontSize: 9, fill: "currentColor", opacity: 0.55 },
        truncate(String(labels[0] || ""), 16)),
      n > 1 ? h("text", {
        key: "l1", x: W - pad, y: H - 8, fontSize: 9,
        fill: "currentColor", opacity: 0.55, textAnchor: "end",
      }, truncate(String(labels[n - 1] || ""), 16)) : null,
    ];
  }

  function lineChart(labels, values, color) {
    var W = 480, H = 150, pad = 26, n = values.length;
    var g = linePoints(values, W, H, pad);
    var pts = values.map(function (v, i) { return g.px(i) + "," + g.py(v); }).join(" ");
    return h("svg", { viewBox: "0 0 " + W + " " + H, style: { width: "100%", height: "auto" } },
      h("polyline", { points: pts, fill: "none", stroke: color, strokeWidth: 2 }),
      values.map(function (v, i) {
        return h("circle", { key: i, cx: g.px(i), cy: g.py(v), r: 3, fill: color });
      }),
      axisText(g.max, labels, n, W, H, pad)
    );
  }

  function areaChart(labels, values, color) {
    var W = 480, H = 150, pad = 26, n = values.length;
    var g = linePoints(values, W, H, pad);
    var line = values.map(function (v, i) { return g.px(i) + "," + g.py(v); }).join(" ");
    var poly = g.px(0) + "," + (H - pad) + " " + line + " " + g.px(n - 1) + "," + (H - pad);
    return h("svg", { viewBox: "0 0 " + W + " " + H, style: { width: "100%", height: "auto" } },
      h("polygon", { points: poly, fill: color, fillOpacity: 0.22, stroke: "none" }),
      h("polyline", { points: line, fill: "none", stroke: color, strokeWidth: 2 }),
      axisText(g.max, labels, n, W, H, pad)
    );
  }

  function pieChart(labels, values) {
    var total = 0;
    values.forEach(function (v) { if (v > 0) total += v; });
    if (total <= 0) {
      return h("div", { style: { fontSize: "0.72rem", opacity: 0.6 } },
        "no positive values to chart");
    }
    var R = 70, CX = 80, CY = 80, angle = -Math.PI / 2;
    var slices = values.map(function (v, i) {
      var frac = (v > 0 ? v : 0) / total;
      var a0 = angle, a1 = angle + frac * 2 * Math.PI;
      angle = a1;
      var large = a1 - a0 > Math.PI ? 1 : 0;
      var d = "M " + CX + " " + CY +
        " L " + (CX + R * Math.cos(a0)) + " " + (CY + R * Math.sin(a0)) +
        " A " + R + " " + R + " 0 " + large + " 1 " +
        (CX + R * Math.cos(a1)) + " " + (CY + R * Math.sin(a1)) + " Z";
      return h("path", {
        key: i, d: d, fill: SLICE_COLORS[i % SLICE_COLORS.length],
        stroke: "#0a0a0a", strokeWidth: 1,
      });
    });
    var legend = h("div", {
        style: { display: "flex", flexWrap: "wrap", gap: "0.15rem 0.7rem", marginTop: "0.35rem" },
      },
      values.map(function (v, i) {
        return h("div", {
            key: i,
            style: { display: "flex", alignItems: "center", gap: "0.3rem", fontSize: "0.68rem" },
          },
          h("span", { style: {
            width: "0.6rem", height: "0.6rem", display: "inline-block",
            background: SLICE_COLORS[i % SLICE_COLORS.length],
          } }),
          truncate(String(labels[i]), 18) + " (" + fmtNum(v) + ")"
        );
      })
    );
    return h("div", null,
      h("svg", {
        viewBox: "0 0 160 160",
        style: { width: "160px", height: "160px", display: "block" },
      }, slices),
      legend
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
    "Donut chart of amount by customer in default.bronze.sd_orders",
    "Radar chart comparing amount by customer",
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
        "Ask about your data — the assistant inspects schemas, runs queries " +
          "and draws charts you can pin to a dashboard. The conversation is continuable."),
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
