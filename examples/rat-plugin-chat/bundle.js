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

  function ToolCallCard(props) {
    var c = props.call, r = props.result;
    var openInit = false; // collapsed by default — output can be huge
    var st = useState(openInit), open = st[0], setOpen = st[1];
    var args = "";
    try { args = JSON.stringify(JSON.parse(c.function.arguments), null, 2); }
    catch (e) { args = c.function.arguments || ""; }
    var output = r ? r.output : null;
    var isErr = r && r.is_error;

    return h("div", {
      style: {
        margin: "8px 0", border: "1px solid " + (isErr ? C.danger : C.border),
        background: C.cardAlt, padding: 10, fontSize: 12,
      },
    },
      h("div", {
        style: { display: "flex", alignItems: "center", gap: 8, cursor: "pointer", color: isErr ? C.danger : C.accent },
        onClick: function () { setOpen(!open); },
      },
        h("span", { style: { fontWeight: 700 } }, isErr ? "✗ tool" : "→ tool"),
        h("span", { style: { fontFamily: "monospace" } }, c.function.name),
        r === null && h("span", { style: { color: C.muted } }, " · running…"),
        r && h("span", { style: { color: C.muted, marginLeft: "auto" } }, open ? "▾ hide" : "▸ show"),
      ),
      open && h("div", { style: { marginTop: 8 } },
        h("div", { style: { color: C.muted, fontSize: 10, textTransform: "uppercase", letterSpacing: 1 } }, "arguments"),
        h("pre", {
          style: {
            margin: "4px 0 8px", padding: 8, background: C.bg, color: C.fg,
            overflow: "auto", maxHeight: 200, fontSize: 11,
          },
        }, args || "(none)"),
        r && h(React.Fragment, null,
          h("div", { style: { color: C.muted, fontSize: 10, textTransform: "uppercase", letterSpacing: 1 } }, "output"),
          h("pre", {
            style: {
              margin: "4px 0 0", padding: 8, background: C.bg, color: isErr ? C.danger : C.fg,
              overflow: "auto", maxHeight: 400, fontSize: 11, whiteSpace: "pre-wrap",
            },
          }, typeof output === "string" ? output : JSON.stringify(output, null, 2)),
        ),
      ),
    );
  }

  function MessageBubble(props) {
    var msg = props.msg, calls = props.calls;
    var isUser = msg.role === "user";
    var label = isUser ? "you" : (msg.role === "assistant" ? "assistant" : msg.role);
    var color = isUser ? C.primary : C.fg;
    return h("div", {
      style: {
        margin: "10px 0", padding: "8px 12px",
        borderLeft: "3px solid " + (isUser ? C.primary : C.accent),
        background: C.card, color: C.fg,
      },
    },
      h("div", { style: { color: C.muted, fontSize: 10, textTransform: "uppercase", letterSpacing: 1, marginBottom: 4 } },
        label),
      msg.content && h("div", { style: { whiteSpace: "pre-wrap", color: color, fontSize: 13, lineHeight: 1.5 } }, msg.content),
      calls && calls.length > 0 && h("div", null,
        calls.map(function (c) { return h(ToolCallCard, { key: c.call.id, call: c.call, result: c.result }); })
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
    var cancelRef = useRef(null);
    var scrollerRef = useRef(null);

    var refreshServers = useCallback(function () {
      reqJSON("GET", "/servers").then(function (d) {
        setServers((d && d.servers) || []);
      });
    }, []);

    useEffect(function () {
      refreshServers();
      var t = setInterval(refreshServers, 10000);
      return function () { clearInterval(t); };
    }, [refreshServers]);

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

      cancelRef.current = streamChat({ messages: nextMsgs }, function (ev, data) {
        if (ev === "assistant_message") {
          // Capture the full assistant message (may have content + tool_calls)
          // and append it to msgs for the next turn. The transcript shows it
          // as a new bubble with its associated tool cards.
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
          setBusy(false);
        } else if (ev === "error") {
          setError((data && data.error) || "unknown error");
          setBusy(false);
        }
      }, function onDone() {
        setBusy(false);
      }, function onError(msg) {
        setError(msg);
        setBusy(false);
      });
    }, [busy, input, msgs]);

    var resetConversation = useCallback(function () {
      if (cancelRef.current) cancelRef.current();
      setMsgs([]);
      setTranscript([]);
      setError(null);
      setBusy(false);
    }, []);

    var onKeyDown = useCallback(function (e) {
      if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) {
        e.preventDefault();
        send();
      }
    }, [send]);

    var totalTools = servers.reduce(function (a, s) { return a + (s.tools_count || 0); }, 0);

    return h("div", {
      style: {
        display: "flex", flexDirection: "column", height: "calc(100vh - 60px)",
        color: C.fg, background: C.bg,
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
        h("div", { style: { color: C.muted, fontSize: 12 } },
          servers.length + " MCP server" + (servers.length === 1 ? "" : "s") + " · " + totalTools + " tools"),
        h("div", { style: { marginLeft: "auto", display: "flex", gap: 6, flexWrap: "wrap" } },
          servers.map(function (s) { return h(ServerBadge, { key: s.capability, server: s }); }),
        ),
        h("button", {
          onClick: resetConversation,
          style: {
            marginLeft: 8, padding: "4px 10px", background: "transparent",
            border: "1px solid " + C.border, color: C.muted, cursor: "pointer", fontSize: 11,
          },
        }, "reset"),
      ),

      // Conversation scroller
      h("div", {
        ref: scrollerRef,
        style: { flex: 1, overflow: "auto", padding: "16px 20px" },
      },
        transcript.length === 0 && h("div", {
          style: { color: C.muted, fontSize: 13, lineHeight: 1.6, maxWidth: 640 },
        },
          h("div", { style: { fontSize: 18, color: C.fg, marginBottom: 8 } }, "Ask about your data."),
          "The chat can call any MCP server registered through the interconnect. ",
          "Try: ",
          h("em", null, "“what tables do we have in shop?”"),
          ", ",
          h("em", null, "“give me total revenue per month from gold.revenue_by_month”"),
          ", or ",
          h("em", null, "“describe the cosmos namespace”"),
          ".",
        ),
        transcript.map(function (item, i) {
          return h(MessageBubble, { key: i, msg: item.msg, calls: item.calls });
        }),
        busy && h("div", { style: { color: C.muted, fontSize: 12, margin: "10px 0" } },
          "thinking…"),
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
        h("button", {
          onClick: send,
          disabled: busy || !input.trim(),
          style: {
            padding: "0 18px", background: busy ? C.muted : C.primary,
            color: C.bg, border: "none", cursor: busy ? "not-allowed" : "pointer",
            fontWeight: 700, letterSpacing: 1,
          },
        }, busy ? "…" : "SEND"),
      ),
    );
  }

  window.__RAT_REGISTER_PLUGIN({
    name: "chat",
    components: { ChatApp: ChatApp },
  });
})();
