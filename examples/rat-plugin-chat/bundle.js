/*
 * rat-plugin-chat — portal UI bundle (Layer 3).
 *
 * A dedicated /x/chat page: chat with the configured LLM, with any MCP
 * server wired through the interconnect available as tools. The plugin
 * backend streams events over SSE; this UI consumes them and renders each
 * tool call + result as an inline card next to the assistant text.
 *
 * Build-free: no JSX, no bundler. Uses the host portal's window.React +
 * window.__RAT_REGISTER_PLUGIN.
 */
(function () {
  "use strict";

  var React = window.React;
  if (!React || typeof window.__RAT_REGISTER_PLUGIN !== "function") {
    console.warn("[chat] RAT plugin host not available — skipping");
    return;
  }
  var h = React.createElement;
  var useState = React.useState;
  var useEffect = React.useEffect;
  var useRef = React.useRef;
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
    warn: "hsl(var(--warning, 38 92% 50%))",
    accent: "hsl(var(--accent, 280 60% 50%))",
  };

  // ── API helpers ────────────────────────────────────────────────

  function apiBase() {
    var s = document.querySelector('script[src*="/plugins/chat/ui/bundle.js"]');
    if (s && s.src && s.src.indexOf("/api/v1/") !== -1) {
      return s.src.slice(0, s.src.indexOf("/api/v1/"));
    }
    return window.location.origin;
  }
  var API = apiBase() + "/api/v1/x/chat";

  function reqJSON(method, path) {
    return fetch(API + path, { method: method, headers: {} }).then(function (res) {
      return res.text().then(function (t) {
        try { return JSON.parse(t); } catch (e) { return { error: t || res.statusText }; }
      });
    });
  }

  // streamChat POSTs the conversation and yields SSE events to onEvent.
  // Returns a "cancel" function the caller can use to abort the request.
  function streamChat(body, onEvent, onDone, onError) {
    var ctrl = new AbortController();
    fetch(API + "/chat", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body),
      signal: ctrl.signal,
    }).then(function (res) {
      if (!res.ok || !res.body) {
        return res.text().then(function (t) { onError(t || ("HTTP " + res.status)); });
      }
      var reader = res.body.getReader();
      var decoder = new TextDecoder();
      var buf = "";
      function pump() {
        return reader.read().then(function (chunk) {
          if (chunk.done) { onDone(); return; }
          buf += decoder.decode(chunk.value, { stream: true });
          // SSE messages are separated by double newlines.
          var parts = buf.split("\n\n");
          buf = parts.pop() || "";
          parts.forEach(function (raw) {
            var ev = "message", data = "";
            raw.split("\n").forEach(function (ln) {
              if (ln.indexOf("event: ") === 0) ev = ln.slice(7).trim();
              else if (ln.indexOf("data: ") === 0) data += ln.slice(6);
            });
            if (!data) return;
            try { onEvent(ev, JSON.parse(data)); }
            catch (e) { onEvent(ev, { raw: data }); }
          });
          return pump();
        });
      }
      return pump();
    }).catch(function (e) {
      if (e && e.name === "AbortError") return;
      onError((e && e.message) || String(e));
    });
    return function () { ctrl.abort(); };
  }

  // ── Tiny markdown renderer ─────────────────────────────────────
  // Bundle is build-free (no node_modules), so we hand-render the subset of
  // markdown the LLM actually emits: tables, headings, bold/italic, inline
  // code, code fences, lists, links. ~100 lines instead of vendoring marked.

  function escapeHtmlText(s) { return s; } // React.createElement escapes by default

  // Render *inline* markdown (bold, italic, code, links) inside one piece of
  // text. Returns an array of React nodes the caller can splat into a parent.
  function renderInline(text, keyPrefix) {
    var out = [];
    var i = 0, n = text.length, key = 0;
    function push(node) { out.push(node); }
    function k() { key++; return (keyPrefix || "i") + "-" + key; }

    while (i < n) {
      // Inline code: `…`
      if (text[i] === "`") {
        var end = text.indexOf("`", i + 1);
        if (end > i) {
          push(h("code", { key: k(), style: {
            background: C.bg, border: "1px solid " + C.border, padding: "1px 4px",
            fontFamily: "monospace", fontSize: "0.92em", color: C.primary,
          } }, text.slice(i + 1, end)));
          i = end + 1; continue;
        }
      }
      // Bold: **…**
      if (text[i] === "*" && text[i + 1] === "*") {
        var endB = text.indexOf("**", i + 2);
        if (endB > i + 1) {
          push(h("strong", { key: k(), style: { color: C.fg } }, renderInline(text.slice(i + 2, endB), k())));
          i = endB + 2; continue;
        }
      }
      // Italic: _…_  or *…* (single)
      if ((text[i] === "_" || text[i] === "*") && text[i + 1] !== text[i]) {
        var mark = text[i];
        var endI = text.indexOf(mark, i + 1);
        if (endI > i) {
          push(h("em", { key: k() }, renderInline(text.slice(i + 1, endI), k())));
          i = endI + 1; continue;
        }
      }
      // Link: [label](url)
      if (text[i] === "[") {
        var lEnd = text.indexOf("]", i + 1);
        if (lEnd > i && text[lEnd + 1] === "(") {
          var uEnd = text.indexOf(")", lEnd + 2);
          if (uEnd > lEnd) {
            var label = text.slice(i + 1, lEnd);
            var url = text.slice(lEnd + 2, uEnd);
            push(h("a", { key: k(), href: url, target: "_blank", rel: "noreferrer",
              style: { color: C.primary, textDecoration: "underline" } }, label));
            i = uEnd + 1; continue;
          }
        }
      }
      // Plain run — eat until the next special character.
      var j = i;
      while (j < n && "`*_[".indexOf(text[j]) < 0) j++;
      if (j === i) j = i + 1; // unmatched special, treat as literal
      push(text.slice(i, j));
      i = j;
    }
    return out;
  }

  // Split markdown text into block tokens and render each. We handle:
  //   - fenced code blocks (```)
  //   - headings (#, ##, …)
  //   - GFM tables (pipe-delimited with a `---|---` separator row)
  //   - unordered lists (- / *)
  //   - ordered lists (1. 2. …)
  //   - paragraphs (anything else)
  function renderMarkdown(text) {
    if (!text) return null;
    var lines = text.replace(/\r\n/g, "\n").split("\n");
    var blocks = [];
    var i = 0, key = 0;
    function k() { key++; return "b-" + key; }

    while (i < lines.length) {
      var line = lines[i];

      // Fenced code block
      if (/^```/.test(line)) {
        var lang = line.slice(3).trim();
        var start = i + 1;
        i = start;
        while (i < lines.length && !/^```/.test(lines[i])) i++;
        var code = lines.slice(start, i).join("\n");
        blocks.push(h("pre", { key: k(), style: {
          background: C.bg, border: "1px solid " + C.border, padding: 10,
          overflow: "auto", fontSize: 12, margin: "8px 0",
        } }, h("code", { className: lang ? "language-" + lang : undefined,
          style: { color: C.fg, fontFamily: "monospace" } }, code)));
        i++; // skip closing fence
        continue;
      }

      // Heading
      var hm = /^(#{1,6})\s+(.*)$/.exec(line);
      if (hm) {
        var lvl = hm[1].length;
        var size = [18, 16, 14, 13, 13, 13][lvl - 1];
        blocks.push(h("h" + lvl, { key: k(), style: {
          margin: "12px 0 6px", fontSize: size, fontWeight: 700,
          color: C.fg, letterSpacing: lvl <= 2 ? 0.5 : 0,
        } }, renderInline(hm[2], k())));
        i++; continue;
      }

      // GFM table — header line, separator (---|---), then rows.
      if (line.indexOf("|") >= 0 && i + 1 < lines.length &&
          /^\s*\|?\s*:?-+:?\s*(\|\s*:?-+:?\s*)+\|?\s*$/.test(lines[i + 1])) {
        var header = splitRow(line);
        i += 2; // skip header + separator
        var rows = [];
        while (i < lines.length && lines[i].indexOf("|") >= 0 && lines[i].trim() !== "") {
          rows.push(splitRow(lines[i]));
          i++;
        }
        blocks.push(h("div", { key: k(), style: { overflow: "auto", margin: "8px 0" } },
          h("table", { style: { borderCollapse: "collapse", fontSize: 12 } },
            h("thead", null,
              h("tr", null, header.map(function (c, j) {
                return h("th", { key: "h" + j, style: {
                  border: "1px solid " + C.border, padding: "4px 8px",
                  textAlign: "left", background: C.cardAlt, color: C.fg,
                  fontWeight: 700,
                } }, renderInline(c, "th-" + j));
              }))),
            h("tbody", null, rows.map(function (r, ri) {
              return h("tr", { key: "r" + ri }, r.map(function (c, ci) {
                return h("td", { key: "c" + ci, style: {
                  border: "1px solid " + C.border, padding: "4px 8px",
                  color: C.fg, whiteSpace: "nowrap",
                } }, renderInline(c, "td-" + ri + "-" + ci));
              }));
            })),
          ),
        ));
        continue;
      }

      // Lists (ordered or unordered) — collect consecutive list-lines.
      if (/^\s*[-*]\s+/.test(line) || /^\s*\d+\.\s+/.test(line)) {
        var ordered = /^\s*\d+\.\s+/.test(line);
        var items = [];
        while (i < lines.length && (/^\s*[-*]\s+/.test(lines[i]) || /^\s*\d+\.\s+/.test(lines[i]))) {
          items.push(lines[i].replace(/^\s*([-*]|\d+\.)\s+/, ""));
          i++;
        }
        var Tag = ordered ? "ol" : "ul";
        blocks.push(h(Tag, { key: k(), style: { margin: "6px 0 6px 20px", color: C.fg, fontSize: 13, lineHeight: 1.5 } },
          items.map(function (it, ii) {
            return h("li", { key: "li-" + ii }, renderInline(it, "li-" + ii));
          })));
        continue;
      }

      // Blank line → flush paragraph break
      if (line.trim() === "") { i++; continue; }

      // Paragraph — gather consecutive non-blank, non-special lines.
      var paraLines = [line];
      i++;
      while (i < lines.length && lines[i].trim() !== "" &&
             !/^```/.test(lines[i]) && !/^#{1,6}\s+/.test(lines[i]) &&
             !/^\s*[-*]\s+/.test(lines[i]) && !/^\s*\d+\.\s+/.test(lines[i]) &&
             !(lines[i].indexOf("|") >= 0 && i + 1 < lines.length &&
               /^\s*\|?\s*:?-+:?\s*(\|\s*:?-+:?\s*)+\|?\s*$/.test(lines[i + 1]))) {
        paraLines.push(lines[i]);
        i++;
      }
      blocks.push(h("p", { key: k(), style: {
        margin: "6px 0", color: C.fg, fontSize: 13, lineHeight: 1.55,
      } }, renderInline(paraLines.join(" "), k())));
    }

    return blocks;
  }

  function splitRow(line) {
    var s = line.trim();
    if (s.charAt(0) === "|") s = s.slice(1);
    if (s.charAt(s.length - 1) === "|") s = s.slice(0, -1);
    return s.split("|").map(function (c) { return c.trim(); });
  }

  // ── Components ─────────────────────────────────────────────────

  function ServerBadge(props) {
    var s = props.server;
    var ok = !s.error;
    return h("div", {
      style: {
        padding: "4px 8px",
        border: "1px solid " + C.border,
        background: ok ? "rgba(34,197,94,0.08)" : "rgba(239,68,68,0.10)",
        fontFamily: "monospace", fontSize: 11, color: C.fg,
        display: "inline-flex", alignItems: "center", gap: 6,
      },
    },
      h("span", { style: { color: ok ? C.primary : C.danger } }, ok ? "●" : "✗"),
      s.name,
      h("span", { style: { color: C.muted } }, " · " + (s.tools_count || 0) + " tools"),
      s.error && h("span", { style: { color: C.danger }, title: s.error }, " · error"),
    );
  }

  // Extract the QueryResult JSON that the SQL MCP server embeds inside its
  // text output. The MCP tool returns "<header>\n\n<json>" — try to peel
  // off the JSON tail and parse it. Returns null if it's not a SQL result.
  function tryParseSqlResult(toolName, output) {
    if (!toolName || (toolName.indexOf("sql__") !== 0)) return null;
    if (typeof output !== "string") return null;
    var idx = output.indexOf("\n{");
    if (idx < 0) return null;
    try {
      var parsed = JSON.parse(output.slice(idx + 1));
      if (parsed && Array.isArray(parsed.columns) && Array.isArray(parsed.rows)) {
        var header = output.slice(0, idx).trim();
        return { columns: parsed.columns, rows: parsed.rows, header: header };
      }
    } catch (e) { return null; }
    return null;
  }

  // SqlResultTable renders a parsed QueryResult inline — much nicer than a
  // JSON dump for the user. Capped at 25 rows for display; the LLM still
  // gets all of them via the tool-result message.
  function SqlResultTable(props) {
    var r = props.result;
    var maxRows = 25;
    var rows = r.rows.slice(0, maxRows);
    return h("div", null,
      r.header && h("div", { style: { color: C.muted, fontSize: 11, margin: "0 0 6px" } }, r.header),
      h("div", { style: { overflow: "auto", border: "1px solid " + C.border } },
        h("table", { style: { borderCollapse: "collapse", fontSize: 11, width: "100%" } },
          h("thead", null,
            h("tr", null, r.columns.map(function (c, i) {
              return h("th", { key: "h" + i, style: {
                border: "1px solid " + C.border, padding: "4px 8px",
                textAlign: "left", background: C.cardAlt, color: C.fg, fontWeight: 700,
              } },
                c.name,
                h("span", { style: { color: C.muted, fontWeight: 400, marginLeft: 6, fontSize: 10 } }, c.type || ""),
              );
            }))),
          h("tbody", null, rows.map(function (row, ri) {
            return h("tr", { key: "r" + ri }, r.columns.map(function (c, ci) {
              var v = row[c.name];
              return h("td", { key: "c" + ci, style: {
                border: "1px solid " + C.border, padding: "4px 8px",
                color: C.fg, whiteSpace: "nowrap",
              } }, v === null || v === undefined ? h("span", { style: { color: C.muted } }, "NULL") : String(v));
            }));
          })),
        ),
      ),
      r.rows.length > maxRows && h("div", { style: { color: C.muted, fontSize: 11, marginTop: 6 } },
        "showing " + maxRows + " of " + r.rows.length + " rows (AI got all of them)"),
    );
  }

  // queryUrlFor builds a portal /query URL that pre-loads the AI's SQL.
  // Uses window.location.origin (the portal) rather than apiBase() (which
  // points at ratd's API host — a different port — and would 404 here).
  function queryUrlFor(sql) {
    return window.location.origin + "/query?sql=" + encodeURIComponent(sql);
  }

  // Subagent calls are tool calls whose name starts with "agent__".
  // We render them differently: header shows the subagent's name, and
  // the output renders as full markdown (since the subagent returns
  // free-form prose, not a JSON dump).
  function isSubagentCall(name) {
    return typeof name === "string" && name.indexOf("agent__") === 0;
  }
  function subagentIdOf(name) {
    return name.indexOf("agent__") === 0 ? name.slice(7) : "";
  }

  // SubagentTraceTimeline lazy-loads /subagent-runs/{id} and renders the
  // subagent's full inner timeline: its own tool calls (which can be
  // nested subagents themselves), its intermediate assistant messages,
  // and any errors. Like Claude's "Show thinking / Show tool use".
  function SubagentTraceTimeline(props) {
    var runId = props.runId;
    var st = useState(null), run = st[0], setRun = st[1];
    var stE = useState(null), err = stE[0], setErr = stE[1];
    var stL = useState(false), loading = stL[0], setLoading = stL[1];

    useEffect(function () {
      if (!runId) return;
      setLoading(true);
      fetch(API + "/subagent-runs/" + encodeURIComponent(runId))
        .then(function (r) { return r.text().then(function (t) { return { ok: r.ok, body: t }; }); })
        .then(function (x) {
          if (!x.ok) { setErr("trace not found (yet?)"); setLoading(false); return; }
          try { setRun(JSON.parse(x.body)); } catch (e) { setErr("malformed trace"); }
          setLoading(false);
        })
        .catch(function (e) { setErr((e && e.message) || String(e)); setLoading(false); });
    }, [runId]);

    if (loading) {
      return h("div", { style: { color: C.muted, fontSize: 11, padding: "6px 0" } }, "loading subagent trace…");
    }
    if (err) {
      return h("div", { style: { color: C.danger, fontSize: 11, padding: "6px 0" } }, err);
    }
    if (!run || !run.events) {
      return h("div", { style: { color: C.muted, fontSize: 11, padding: "6px 0" } }, "no events recorded");
    }

    // Pair tool_call → tool_result by tool_call_id, and collapse the
    // assistant_delta firehose into the matching assistant_message.
    var steps = [];
    var pendingByCallId = {};
    var msgCount = 0, toolCount = 0;
    for (var i = 0; i < run.events.length; i++) {
      var ev = run.events[i];
      var payload = ev.payload || {};
      if (ev.event === "tool_call") {
        steps.push({ kind: "tool", call: payload, result: null });
        pendingByCallId[payload.id] = steps.length - 1;
        toolCount++;
      } else if (ev.event === "tool_result") {
        var idx = pendingByCallId[payload.tool_call_id];
        if (idx !== undefined) steps[idx].result = payload;
      } else if (ev.event === "assistant_message") {
        // Only show assistant messages that have content (skip the
        // "tool-call-only" turns — the tool_call card itself surfaces
        // those).
        if (payload.content) {
          steps.push({ kind: "message", content: payload.content });
          msgCount++;
        }
      } else if (ev.event === "started") {
        steps.unshift({ kind: "started", payload: payload });
      } else if (ev.event === "error") {
        steps.push({ kind: "error", message: (payload && payload.error) || "" });
      }
    }

    return h("div", { style: { padding: "8px 8px 4px 12px", marginTop: 8, borderTop: "1px dashed " + C.border } },
      h("div", { style: {
        color: C.muted, fontSize: 10, textTransform: "uppercase", letterSpacing: 1, marginBottom: 6,
      } },
        "subagent timeline · " + toolCount + " tool call" + (toolCount === 1 ? "" : "s") +
        " · " + msgCount + " message" + (msgCount === 1 ? "" : "s")),
      steps.map(function (s, i) {
        if (s.kind === "started") {
          var agent = s.payload && s.payload.agent;
          return h("div", { key: "step" + i, style: { fontSize: 11, color: C.muted, margin: "4px 0" } },
            "● started",
            agent && h("span", null, " · ", h("strong", { style: { color: C.fg } }, agent.name || agent.id)),
            " · " + (s.payload.tools_available || 0) + " tools available",
          );
        }
        if (s.kind === "tool") {
          // Recursive! A subagent's tool_call can itself be agent__X
          // which would have its own trace at the same conversation.
          return h(ToolCallCard, {
            key: "step" + i, call: s.call, result: s.result,
            agentsById: props.agentsById, conversationID: props.conversationID,
          });
        }
        if (s.kind === "message") {
          return h("div", { key: "step" + i, style: {
            margin: "6px 0", padding: "6px 10px", background: C.card,
            borderLeft: "2px solid " + (props.subagentColor || C.accent), color: C.fg,
          } },
            h("div", { style: { color: C.muted, fontSize: 9, textTransform: "uppercase", letterSpacing: 1, marginBottom: 3 } },
              "subagent message"),
            renderMarkdown(s.content),
          );
        }
        if (s.kind === "error") {
          return h("div", { key: "step" + i, style: { color: C.danger, fontSize: 11, margin: "4px 0" } },
            "✗ " + s.message);
        }
        return null;
      }),
    );
  }

  function ToolCallCard(props) {
    var c = props.call, r = props.result;
    var openInit = false; // collapsed by default — output can be huge
    var st = useState(openInit), open = st[0], setOpen = st[1];
    // Subagent calls have a separate "show work" toggle that lazy-loads
    // the nested trace. Default closed.
    var stTrace = useState(false), traceOpen = stTrace[0], setTraceOpen = stTrace[1];
    var parsedArgs = null;
    try { parsedArgs = JSON.parse(c.function.arguments); } catch (e) {}
    var argsPretty = parsedArgs ? JSON.stringify(parsedArgs, null, 2) : (c.function.arguments || "");
    var output = r ? r.output : null;
    var isErr = r && r.is_error;
    var sqlResult = r && !isErr ? tryParseSqlResult(c.function.name, output) : null;
    var sqlText = parsedArgs && typeof parsedArgs.sql === "string" ? parsedArgs.sql : null;
    var showOpenInQuery = sqlText && sqlText.length > 0;
    var sub = isSubagentCall(c.function.name);
    var subAgent = sub ? (props.agentsById && props.agentsById[subagentIdOf(c.function.name)]) : null;
    var subColor = subAgent && subAgent.color ? subAgent.color : C.accent;
    // Subagent runs are stored at {parent_conv_id}__{tool_call_id}.
    var subRunId = sub && props.conversationID ? (props.conversationID + "__" + c.id) : null;

    var header = sub
      ? h(React.Fragment, null,
          h("span", { style: { fontWeight: 700 } }, isErr ? "✗ subagent" : "✦ subagent"),
          subAgent && h("span", { style: {
            width: 10, height: 10, borderRadius: 2, background: subColor, display: "inline-block",
          } }),
          h("span", { style: { fontWeight: 600 } }, (subAgent && subAgent.name) || subagentIdOf(c.function.name)),
        )
      : h(React.Fragment, null,
          h("span", { style: { fontWeight: 700 } }, isErr ? "✗ tool" : "→ tool"),
          h("span", { style: { fontFamily: "monospace" } }, c.function.name),
        );

    return h("div", {
      style: {
        margin: "8px 0",
        border: "1px solid " + (isErr ? C.danger : C.border),
        borderLeft: sub ? "4px solid " + subColor : "1px solid " + (isErr ? C.danger : C.border),
        background: C.cardAlt, padding: 10, fontSize: 12,
      },
    },
      h("div", {
        style: { display: "flex", alignItems: "center", gap: 8 },
      },
        h("div", {
          style: { display: "flex", alignItems: "center", gap: 8, cursor: "pointer",
                   color: isErr ? C.danger : (sub ? subColor : C.accent), flex: 1 },
          onClick: function () { setOpen(!open); },
        },
          header,
          r === null && h("span", { style: { color: C.muted } }, " · running…"),
          sqlResult && h("span", { style: { color: C.muted, fontSize: 11 } },
            " · " + sqlResult.rows.length + " row" + (sqlResult.rows.length === 1 ? "" : "s")),
          r && h("span", { style: { color: C.muted, marginLeft: "auto" } }, open ? "▾ hide" : "▸ show"),
        ),
        showOpenInQuery && h("a", {
          href: queryUrlFor(sqlText), target: "_blank", rel: "noreferrer",
          onClick: function (e) { e.stopPropagation(); },
          style: {
            fontSize: 10, padding: "2px 8px", border: "1px solid " + C.border,
            color: C.primary, textDecoration: "none", letterSpacing: 0.5,
          },
        }, "OPEN IN QUERY"),
      ),
      // Subagents: show the task as a quoted preview (always visible),
      // and render the full markdown answer inline (not collapsed) since
      // that *is* the chat content the user came for. Plus a separate
      // "show work" toggle that lazy-loads the subagent's full timeline.
      sub && parsedArgs && parsedArgs.task && h("div", {
        style: {
          margin: "6px 0", padding: "4px 8px", borderLeft: "2px solid " + C.border,
          color: C.muted, fontSize: 11, fontStyle: "italic",
        },
      }, "task: " + parsedArgs.task.slice(0, 220) + (parsedArgs.task.length > 220 ? "…" : "")),
      sub && r && typeof output === "string" && h("div", {
        style: { margin: "6px 0 0", padding: 8, background: C.card, color: isErr ? C.danger : C.fg },
      }, isErr ? output : renderMarkdown(output)),
      // The "show work" affordance: opens the nested timeline only when
      // we have a conversation id (and thus can resolve a run id).
      sub && r && subRunId && h("div", { style: { marginTop: 6 } },
        h("button", {
          onClick: function () { setTraceOpen(!traceOpen); },
          style: {
            padding: "3px 8px", background: "transparent",
            border: "1px solid " + C.border, color: C.muted,
            cursor: "pointer", fontSize: 10, letterSpacing: 0.5, textTransform: "uppercase",
          },
        }, traceOpen ? "▾ hide work" : "▸ show subagent's work"),
        traceOpen && h(SubagentTraceTimeline, {
          runId: subRunId, agentsById: props.agentsById,
          conversationID: props.conversationID, subagentColor: subColor,
        }),
      ),
      // Non-subagent: keep the collapsed-by-default behaviour with the
      // SQL table affordance.
      !sub && open && h("div", { style: { marginTop: 8 } },
        h("div", { style: { color: C.muted, fontSize: 10, textTransform: "uppercase", letterSpacing: 1 } }, "arguments"),
        h("pre", {
          style: {
            margin: "4px 0 8px", padding: 8, background: C.bg, color: C.fg,
            overflow: "auto", maxHeight: 200, fontSize: 11,
          },
        }, argsPretty || "(none)"),
        r && h(React.Fragment, null,
          h("div", { style: { color: C.muted, fontSize: 10, textTransform: "uppercase", letterSpacing: 1 } }, "output"),
          sqlResult
            ? h("div", { style: { margin: "4px 0 0" } }, h(SqlResultTable, { result: sqlResult }))
            : h("pre", {
                style: {
                  margin: "4px 0 0", padding: 8, background: C.bg, color: isErr ? C.danger : C.fg,
                  overflow: "auto", maxHeight: 400, fontSize: 11, whiteSpace: "pre-wrap",
                },
              }, typeof output === "string" ? output : JSON.stringify(output, null, 2)),
        ),
      ),
    );
  }

  // StreamingBubble renders the in-progress assistant turn: content arrives
  // token-by-token, reasoning models also send chain-of-thought we show as
  // a muted "thinking…" preamble (collapsed). When the turn finalises we
  // replace this with a normal MessageBubble in the transcript.
  function StreamingBubble(props) {
    var content = props.content || "";
    var reasoning = props.reasoning || "";
    var stTh = useState(false), thinkingOpen = stTh[0], setThinkingOpen = stTh[1];
    return h("div", {
      style: {
        margin: "10px 0", padding: "8px 12px",
        borderLeft: "3px dashed " + C.accent,
        background: C.card, color: C.fg,
      },
    },
      h("div", { style: { color: C.muted, fontSize: 10, textTransform: "uppercase", letterSpacing: 1, marginBottom: 4 } },
        "assistant · streaming"),
      reasoning && h("div", {
        style: { color: C.muted, fontSize: 11, marginBottom: 6, cursor: "pointer" },
        onClick: function () { setThinkingOpen(!thinkingOpen); },
      },
        thinkingOpen ? "▾ thinking" : "▸ thinking",
        thinkingOpen && h("pre", {
          style: {
            margin: "4px 0 0", padding: 8, background: C.bg, color: C.muted,
            fontSize: 11, whiteSpace: "pre-wrap", maxHeight: 200, overflow: "auto",
          },
        }, reasoning),
      ),
      content
        ? h("div", { style: { color: C.fg } }, renderMarkdown(content))
        : !reasoning && h("div", { style: { color: C.muted, fontSize: 12 } }, "thinking…"),
    );
  }

  function MessageBubble(props) {
    var msg = props.msg, calls = props.calls;
    var isUser = msg.role === "user";
    var label = isUser ? "you" : (msg.role === "assistant" ? "assistant" : msg.role);
    var color = isUser ? C.primary : C.fg;
    var accentColor = isUser ? C.primary : (props.agentColor || C.accent);
    return h("div", {
      style: {
        margin: "10px 0", padding: "8px 12px",
        borderLeft: "3px solid " + accentColor,
        background: C.card, color: C.fg,
      },
    },
      h("div", { style: { color: C.muted, fontSize: 10, textTransform: "uppercase", letterSpacing: 1, marginBottom: 4 } },
        label),
      msg.content && (isUser
        ? h("div", { style: { whiteSpace: "pre-wrap", color: color, fontSize: 13, lineHeight: 1.5 } }, msg.content)
        : h("div", { style: { color: C.fg } }, renderMarkdown(msg.content))),
      calls && calls.length > 0 && h("div", null,
        calls.map(function (c) {
          return h(ToolCallCard, {
            key: c.call.id, call: c.call, result: c.result,
            agentsById: props.agentsById,
            conversationID: props.conversationID,
          });
        })
      ),
    );
  }

  // ── Main component ────────────────────────────────────────────

  function ChatApp() {
    var st1 = useState([]),    msgs = st1[0], setMsgs = st1[1];      // conversation history (for AI)
    var st2 = useState([]),    transcript = st2[0], setTranscript = st2[1]; // UI view: list of {msg, calls?}
    var st3 = useState(""),    input = st3[0], setInput = st3[1];
    var st4 = useState(false), busy = st4[0], setBusy = st4[1];
    var st5 = useState([]),    servers = st5[0], setServers = st5[1];
    var st6 = useState(null),  error = st6[0], setError = st6[1];
    // streamingBubble is the in-progress assistant turn (per LLM call in
    // the multi-turn loop). Cleared when the turn finalises (assistant_message
    // event) and a fresh bubble appears for the next iteration after tools run.
    var st7 = useState(null),  streamingBubble = st7[0], setStreamingBubble = st7[1];
    var st8 = useState([]),    agents = st8[0], setAgents = st8[1];
    // Selected agent id. "" = no agent (chat plugin's default prompt + all tools).
    var st9 = useState(""),    agentID = st9[0], setAgentID = st9[1];
    // Conversation state. conversationID is the persisted id (set when the
    // server creates one mid-turn or when we load an existing one).
    // conversations is the sidebar list (summaries, no messages).
    var st10 = useState([]),   conversations = st10[0], setConversations = st10[1];
    var st11 = useState(""),   conversationID = st11[0], setConversationID = st11[1];
    var st12 = useState(""),   conversationTitle = st12[0], setConversationTitle = st12[1];
    var st13 = useState(true), sidebarOpen = st13[0], setSidebarOpen = st13[1];
    var cancelRef = useRef(null);
    var scrollerRef = useRef(null);
    var streamingBufRef = useRef({ content: "", reasoning: "" });

    var refreshServers = useCallback(function () {
      reqJSON("GET", "/servers").then(function (d) {
        setServers((d && d.servers) || []);
      });
    }, []);

    var refreshAgents = useCallback(function () {
      reqJSON("GET", "/agents").then(function (d) {
        var list = (d && d.agents) || [];
        setAgents(list);
        // If user hasn't picked an agent yet, default to "generalist" if
        // it exists, otherwise the first one in the catalog.
        setAgentID(function (cur) {
          if (cur) return cur;
          var gen = list.find(function (a) { return a.id === "generalist"; });
          if (gen) return gen.id;
          return list.length > 0 ? list[0].id : "";
        });
      });
    }, []);

    var refreshConversations = useCallback(function () {
      reqJSON("GET", "/conversations").then(function (d) {
        setConversations((d && d.conversations) || []);
      });
    }, []);

    useEffect(function () {
      refreshServers();
      refreshAgents();
      refreshConversations();
      var t = setInterval(function () { refreshServers(); refreshAgents(); refreshConversations(); }, 10000);
      return function () { clearInterval(t); };
    }, [refreshServers, refreshAgents, refreshConversations]);

    // Load an existing conversation: pull full messages from the server,
    // reconstruct the transcript view, set the agent + id + title.
    var loadConversation = useCallback(function (id) {
      if (cancelRef.current) cancelRef.current();
      setError(null);
      setStreamingBubble(null);
      setBusy(false);
      reqJSON("GET", "/conversations/" + id).then(function (c) {
        if (!c || c.error) { setError((c && c.error) || "load failed"); return; }
        setConversationID(c.id);
        setConversationTitle(c.title || "");
        if (c.agent_id) setAgentID(c.agent_id);
        var allMessages = c.messages || [];
        setMsgs(allMessages);
        // Reconstruct the transcript: each user/assistant message is one
        // bubble; tool messages attach to the previous assistant bubble as
        // tool_call+result pairs.
        var view = [];
        for (var i = 0; i < allMessages.length; i++) {
          var m = allMessages[i];
          if (m.role === "user") {
            view.push({ msg: m });
          } else if (m.role === "assistant") {
            var calls = (m.tool_calls || []).map(function (tc) { return { call: tc, result: null }; });
            view.push({ msg: m, calls: calls });
          } else if (m.role === "tool") {
            // Attach to the closest preceding assistant bubble with a matching call id.
            for (var j = view.length - 1; j >= 0; j--) {
              if (view[j].msg && view[j].msg.role === "assistant" && view[j].calls) {
                for (var k = 0; k < view[j].calls.length; k++) {
                  if (view[j].calls[k].call.id === m.tool_call_id) {
                    view[j].calls[k].result = {
                      tool_call_id: m.tool_call_id, name: m.name,
                      output: m.content, is_error: false,
                    };
                    break;
                  }
                }
                break;
              }
            }
          }
        }
        setTranscript(view);
      });
    }, []);

    var newChat = useCallback(function () {
      if (cancelRef.current) cancelRef.current();
      setConversationID("");
      setConversationTitle("");
      setMsgs([]);
      setTranscript([]);
      setError(null);
      setStreamingBubble(null);
      setBusy(false);
    }, []);

    var renameConversation = useCallback(function (id, currentTitle) {
      var next = window.prompt("Rename conversation:", currentTitle || "");
      if (next === null) return;
      next = next.trim();
      if (!next) return;
      reqJSON("PATCH", "/conversations/" + id, { title: next }).then(function () {
        refreshConversations();
        if (id === conversationID) setConversationTitle(next);
      });
    }, [conversationID, refreshConversations]);

    var deleteConversation = useCallback(function (id, title) {
      if (!window.confirm("Delete \"" + (title || id) + "\"? This can't be undone.")) return;
      fetch(API + "/conversations/" + id, { method: "DELETE" }).then(function () {
        refreshConversations();
        if (id === conversationID) newChat();
      });
    }, [conversationID, newChat, refreshConversations]);

    useEffect(function () {
      // Auto-scroll to bottom when transcript grows.
      var el = scrollerRef.current;
      if (el) el.scrollTop = el.scrollHeight;
    }, [transcript]);

    var send = useCallback(function () {
      var text = input.trim();
      if (!text || busy) return;
      var userMsg = { role: "user", content: text };
      var nextMsgs = msgs.concat([userMsg]);
      setMsgs(nextMsgs);
      setTranscript(function (prev) { return prev.concat([{ msg: userMsg }]); });
      setInput("");
      setBusy(true);
      setError(null);

      // We accumulate tool calls and pair them with results as events arrive.
      var pendingCalls = []; // [{call, result}]
      var currentAssistantIdx = null;

      streamingBufRef.current = { content: "", reasoning: "" };
      setStreamingBubble(null);

      cancelRef.current = streamChat({
        messages: nextMsgs, agent_id: agentID,
        conversation_id: conversationID || undefined,
      }, function (ev, data) {
        if (ev === "conversation") {
          if (data && data.id) {
            setConversationID(data.id);
            if (data.title) setConversationTitle(data.title);
            // Refresh the sidebar so the new (or updated) row appears.
            refreshConversations();
          }
          return;
        }
        if (ev === "assistant_delta") {
          // Per-token streaming. Append content / reasoning to the running
          // streamingBubble — the StreamingBubble component renders it live.
          var buf = streamingBufRef.current;
          if (data && data.content) buf.content += data.content;
          if (data && data.reasoning) buf.reasoning += data.reasoning;
          setStreamingBubble({ content: buf.content, reasoning: buf.reasoning });
        } else if (ev === "assistant_message") {
          // Final assembled message for this turn — push as a real bubble
          // and clear the streamingBubble so the next iteration's deltas
          // start fresh.
          streamingBufRef.current = { content: "", reasoning: "" };
          setStreamingBubble(null);
          setMsgs(function (cur) { return cur.concat([data]); });
          pendingCalls = (data.tool_calls || []).map(function (tc) { return { call: tc, result: null }; });
          setTranscript(function (prev) {
            var copy = prev.concat([{ msg: data, calls: pendingCalls }]);
            currentAssistantIdx = copy.length - 1;
            return copy;
          });
        } else if (ev === "tool_call") {
          // Already shown via assistant_message in most cases; this event is
          // a chance for models that stream calls separately. Ensure it's in
          // pendingCalls if not.
          var present = pendingCalls.some(function (c) { return c.call.id === data.id; });
          if (!present) {
            pendingCalls.push({ call: data, result: null });
            setTranscript(function (prev) {
              if (currentAssistantIdx === null) return prev;
              var copy = prev.slice();
              copy[currentAssistantIdx] = Object.assign({}, copy[currentAssistantIdx], { calls: pendingCalls.slice() });
              return copy;
            });
          }
        } else if (ev === "tool_result") {
          // Pair the result with its call by id.
          pendingCalls = pendingCalls.map(function (c) {
            return c.call.id === data.tool_call_id ? Object.assign({}, c, { result: data }) : c;
          });
          setTranscript(function (prev) {
            if (currentAssistantIdx === null) return prev;
            var copy = prev.slice();
            copy[currentAssistantIdx] = Object.assign({}, copy[currentAssistantIdx], { calls: pendingCalls.slice() });
            return copy;
          });
          // Append the tool-role message to msgs so subsequent /chat calls
          // include it. The orchestrator already appends server-side; this
          // mirrors it client-side so a reconnect keeps full history.
          setMsgs(function (cur) {
            return cur.concat([{
              role: "tool",
              tool_call_id: data.tool_call_id,
              name: data.name,
              content: typeof data.output === "string" ? data.output : JSON.stringify(data.output),
            }]);
          });
        } else if (ev === "done") {
          setStreamingBubble(null);
          setBusy(false);
        } else if (ev === "error") {
          setError((data && data.error) || "unknown error");
          setStreamingBubble(null);
          setBusy(false);
        }
      }, function onDone() {
        setStreamingBubble(null);
        setBusy(false);
      }, function onError(msg) {
        setError(msg);
        setStreamingBubble(null);
        setBusy(false);
      });
    }, [busy, input, msgs, agentID, conversationID, refreshConversations]);

    var cancelTurn = useCallback(function () {
      if (cancelRef.current) cancelRef.current();
      setStreamingBubble(null);
      setBusy(false);
    }, []);

    var resetConversation = useCallback(function () {
      if (cancelRef.current) cancelRef.current();
      setMsgs([]);
      setTranscript([]);
      setError(null);
      setStreamingBubble(null);
      setBusy(false);
    }, []);

    var onKeyDown = useCallback(function (e) {
      if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) {
        e.preventDefault();
        send();
      }
    }, [send]);

    var totalTools = servers.reduce(function (a, s) { return a + (s.tools_count || 0); }, 0);
    var agentsById = {};
    agents.forEach(function (a) { agentsById[a.id] = a; });
    // Filter the picker to enabled agents only — disabled ones still
    // resolve via /agents (for in-flight conversations) but can't be
    // freshly picked.
    var pickerAgents = agents.filter(function (a) { return !a.disabled; });
    var currentAgent = agentsById[agentID];
    var examples = (currentAgent && currentAgent.example_questions) || [];

    // Format a date as a compact relative string for the sidebar.
    function relativeTime(iso) {
      if (!iso) return "";
      var t = new Date(iso).getTime();
      var diff = (Date.now() - t) / 1000;
      if (diff < 60) return Math.round(diff) + "s";
      if (diff < 3600) return Math.round(diff / 60) + "m";
      if (diff < 86400) return Math.round(diff / 3600) + "h";
      return Math.round(diff / 86400) + "d";
    }

    return h("div", {
      style: {
        display: "flex", flexDirection: "row", height: "calc(100vh - 60px)",
        color: C.fg, background: C.bg,
      },
    },
      // ── Conversation sidebar ──────────────────────────────
      sidebarOpen && h("div", {
        style: {
          width: 240, borderRight: "1px solid " + C.border,
          background: C.card, display: "flex", flexDirection: "column",
        },
      },
        h("div", {
          style: { padding: "10px 12px", borderBottom: "1px solid " + C.border, display: "flex", gap: 6 },
        },
          h("button", {
            onClick: newChat,
            style: {
              flex: 1, padding: "6px 10px", background: C.primary, color: C.bg,
              border: "none", cursor: "pointer", fontWeight: 700, letterSpacing: 0.5, fontSize: 12,
            },
          }, "+ NEW CHAT"),
          h("button", {
            onClick: function () { setSidebarOpen(false); },
            title: "Hide sidebar",
            style: {
              padding: "6px 8px", background: "transparent", color: C.muted,
              border: "1px solid " + C.border, cursor: "pointer", fontSize: 12,
            },
          }, "«"),
        ),
        h("div", { style: { flex: 1, overflow: "auto", padding: "6px 0" } },
          conversations.length === 0 && h("div", {
            style: { color: C.muted, fontSize: 11, padding: "10px 12px", lineHeight: 1.5 },
          }, "No conversations yet. Send a message to start one — it'll appear here automatically."),
          conversations.map(function (c) {
            var selected = c.id === conversationID;
            var ag = agentsById[c.agent_id];
            return h("div", {
              key: c.id,
              onClick: function () { loadConversation(c.id); },
              style: {
                padding: "8px 12px", cursor: "pointer",
                borderLeft: "3px solid " + (selected ? (ag && ag.color ? ag.color : C.primary) : "transparent"),
                background: selected ? "rgba(255,255,255,0.04)" : "transparent",
              },
              onMouseEnter: function (e) { if (!selected) e.currentTarget.style.background = "rgba(255,255,255,0.02)"; },
              onMouseLeave: function (e) { if (!selected) e.currentTarget.style.background = "transparent"; },
            },
              h("div", { style: { display: "flex", alignItems: "center", gap: 4 } },
                ag && ag.color && h("span", {
                  style: { width: 8, height: 8, borderRadius: 2, background: ag.color, display: "inline-block" },
                }),
                h("span", { style: { fontSize: 12, fontWeight: 600, flex: 1, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" } },
                  c.title || "(untitled)"),
                h("span", { style: { fontSize: 10, color: C.muted } }, relativeTime(c.updated_at)),
              ),
              h("div", { style: { display: "flex", alignItems: "center", gap: 6, marginTop: 2 } },
                h("span", { style: { fontSize: 10, color: C.muted, flex: 1 } },
                  (ag ? ag.name : c.agent_id || "no agent") + " · " + c.message_count + " msg"),
                h("button", {
                  onClick: function (e) { e.stopPropagation(); renameConversation(c.id, c.title); },
                  title: "Rename",
                  style: {
                    padding: "0 4px", background: "transparent", color: C.muted,
                    border: "none", cursor: "pointer", fontSize: 11,
                  },
                }, "✎"),
                h("button", {
                  onClick: function (e) { e.stopPropagation(); deleteConversation(c.id, c.title); },
                  title: "Delete",
                  style: {
                    padding: "0 4px", background: "transparent", color: C.danger,
                    border: "none", cursor: "pointer", fontSize: 11,
                  },
                }, "×"),
              ),
            );
          }),
        ),
      ),

      // Show the toggle when sidebar is hidden.
      !sidebarOpen && h("button", {
        onClick: function () { setSidebarOpen(true); },
        title: "Show conversation history",
        style: {
          position: "absolute", left: 0, top: 70, padding: "8px 6px",
          background: C.card, color: C.muted, border: "1px solid " + C.border,
          borderLeft: "none", cursor: "pointer", fontSize: 14, zIndex: 5,
        },
      }, "»"),

    // ── Main chat column ────────────────────────────────────
    h("div", {
      style: {
        flex: 1, display: "flex", flexDirection: "column",
        color: C.fg, background: C.bg, minWidth: 0,
      },
    },
      // Header
      h("div", {
        style: {
          padding: "12px 20px", borderBottom: "1px solid " + C.border,
          display: "flex", alignItems: "center", gap: 12, flexWrap: "wrap",
        },
      },
        h("div", { style: { fontWeight: 700, fontSize: 14, letterSpacing: 1 } }, "CHAT"),
        conversationTitle && h("div", {
          style: { color: C.muted, fontSize: 12, fontStyle: "italic",
                   maxWidth: 320, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" },
        }, "· " + conversationTitle),
        // Agent picker — the LLM persona for this conversation. Changing
        // it mid-conversation just affects the *next* turn; we don't
        // mutate prior messages. "(no agent)" means use the plugin's
        // default system prompt + all tools.
        pickerAgents.length > 0 && h("div", { style: { display: "flex", alignItems: "center", gap: 6 } },
          h("span", { style: { color: C.muted, fontSize: 11, textTransform: "uppercase", letterSpacing: 1 } }, "agent"),
          currentAgent && currentAgent.color && h("span", {
            style: { width: 10, height: 10, borderRadius: 2, background: currentAgent.color, display: "inline-block" },
          }),
          h("select", {
            value: agentID,
            onChange: function (e) { setAgentID(e.target.value); },
            disabled: busy,
            title: "Switching mid-conversation affects only the next turn",
            style: {
              padding: "4px 8px", background: C.card, color: C.fg,
              border: "1px solid " + C.border, fontSize: 12, fontFamily: "inherit",
              cursor: busy ? "not-allowed" : "pointer",
            },
          },
            h("option", { value: "" }, "(no agent — defaults)"),
            pickerAgents.map(function (a) {
              var toolCount = (a.allowed_tools && a.allowed_tools[0] === "*")
                ? "all tools"
                : ((a.allowed_tools && a.allowed_tools.length) || 0) + " tools";
              var sub = (a.subagents && a.subagents.length) || 0;
              var label = a.name + "  ·  " + toolCount + (sub > 0 ? "  ·  " + sub + " subagent" + (sub === 1 ? "" : "s") : "");
              return h("option", { key: a.id, value: a.id }, label);
            }),
          ),
        ),
        h("div", { style: { color: C.muted, fontSize: 12 } },
          servers.length + " MCP server" + (servers.length === 1 ? "" : "s") + " · " + totalTools + " tools"),
        h("div", { style: { marginLeft: "auto", display: "flex", gap: 6, flexWrap: "wrap" } },
          servers.map(function (s) { return h(ServerBadge, { key: s.capability, server: s }); }),
        ),
        h("button", {
          onClick: newChat,
          title: "Start a new conversation",
          style: {
            marginLeft: 8, padding: "4px 10px", background: "transparent",
            border: "1px solid " + C.border, color: C.muted, cursor: "pointer", fontSize: 11,
          },
        }, "new chat"),
      ),

      // Conversation scroller
      h("div", {
        ref: scrollerRef,
        style: { flex: 1, overflow: "auto", padding: "16px 20px" },
      },
        transcript.length === 0 && h("div", {
          style: { color: C.muted, fontSize: 13, lineHeight: 1.6, maxWidth: 720 },
        },
          h("div", { style: { fontSize: 18, color: C.fg, marginBottom: 8 } }, "Ask about your data."),
          currentAgent
            ? h("div", null,
                "Currently: ",
                h("span", { style: { color: currentAgent.color || C.fg, fontWeight: 700 } }, currentAgent.name),
                currentAgent.description && h("span", { style: { color: C.muted } }, " — " + currentAgent.description),
              )
            : h("div", null, "Pick an agent in the header or use defaults — chat can call any MCP server wired through the interconnect."),
          // Example-question chips (per agent). One click submits.
          examples.length > 0 && h("div", { style: { marginTop: 14, display: "flex", flexWrap: "wrap", gap: 8 } },
            examples.map(function (q, i) {
              return h("button", {
                key: i, type: "button",
                onClick: function () { setInput(q); setTimeout(send, 0); },
                style: {
                  padding: "6px 10px", background: C.card, color: C.fg,
                  border: "1px solid " + C.border, cursor: "pointer", fontSize: 12,
                  textAlign: "left",
                },
              }, "→ " + q);
            }),
          ),
        ),
        transcript.map(function (item, i) {
          return h(MessageBubble, {
            key: i, msg: item.msg, calls: item.calls,
            agentColor: currentAgent && currentAgent.color, agentsById: agentsById,
            conversationID: conversationID,
          });
        }),
        streamingBubble && h(StreamingBubble, {
          content: streamingBubble.content, reasoning: streamingBubble.reasoning,
        }),
        error && h("div", {
          style: {
            margin: "10px 0", padding: "8px 12px", background: "rgba(239,68,68,0.10)",
            color: C.danger, fontSize: 12, border: "1px solid " + C.danger,
          },
        }, "error: " + error),
      ),

      // Composer
      h("div", { style: { borderTop: "1px solid " + C.border, padding: "12px 20px", display: "flex", gap: 8 } },
        h("textarea", {
          value: input,
          onChange: function (e) { setInput(e.target.value); },
          onKeyDown: onKeyDown,
          placeholder: "Ask about your data… (⌘+Enter to send)",
          style: {
            flex: 1, minHeight: 50, maxHeight: 200, padding: 10, background: C.card,
            color: C.fg, border: "1px solid " + C.border, fontFamily: "inherit",
            fontSize: 13, resize: "vertical",
          },
        }),
        busy
          ? h("button", {
              onClick: cancelTurn,
              style: {
                padding: "0 18px", background: C.danger,
                color: C.fg, border: "none", cursor: "pointer",
                fontWeight: 700, letterSpacing: 1,
              },
              title: "Cancel this turn",
            }, "STOP")
          : h("button", {
              onClick: send,
              disabled: !input.trim(),
              style: {
                padding: "0 18px", background: !input.trim() ? C.muted : C.primary,
                color: C.bg, border: "none", cursor: !input.trim() ? "not-allowed" : "pointer",
                fontWeight: 700, letterSpacing: 1,
              },
            }, "SEND"),
      ), // end composer
    ),   // end main chat column h("div")
    );   // end outer flex-row h("div") + return
  }

  window.__RAT_REGISTER_PLUGIN("chat", {
    navItems: [{ label: "Chat", icon: "message-circle", href: "/x/chat", priority: 5 }],
    routes: [{ path: "/x/chat", component: ChatApp }],
  });
  console.info("[chat] registered with the portal");
})();
