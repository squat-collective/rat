/*
 * rat-plugin-ai — portal UI bundle (Layer 3): the AI Data Navigator chat page.
 *
 * Build-free: the portal exposes React on window.React and a registration hook
 * on window.__RAT_REGISTER_PLUGIN. This bundle registers an /x/ai chat page and
 * a sidebar nav item.
 */
(function () {
  "use strict";

  var React = window.React;
  if (!React || typeof window.__RAT_REGISTER_PLUGIN !== "function") {
    console.warn("[ai] RAT plugin host not available — skipping");
    return;
  }
  var h = React.createElement;

  // Discover the API base from this bundle's own <script src>.
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

  // One chat bubble — user, assistant, or error.
  function Message(props) {
    var m = props.m;
    var isUser = m.role === "user";
    var isErr = m.role === "error";
    var label = isUser ? "you" : isErr ? "error" : "assistant";
    return h("div", {
        className: "brutal-card",
        style: {
          padding: "0.75rem",
          alignSelf: isUser ? "flex-end" : "flex-start",
          maxWidth: "85%",
          borderColor: isErr ? "#f87171" : undefined,
        },
      },
      h("div", {
        style: {
          fontSize: "0.6rem", fontWeight: "bold", letterSpacing: "0.08em",
          textTransform: "uppercase", opacity: 0.55, marginBottom: "0.35rem",
        },
      }, label),
      (m.steps && m.steps.length)
        ? h("div", {
            style: {
              fontSize: "0.7rem", opacity: 0.6, marginBottom: "0.45rem",
              fontFamily: "monospace",
            },
          }, m.steps.map(function (s, j) {
            return h("div", { key: j }, "🔧 " + s.tool + "(" + truncate(s.args, 90) + ")");
          }))
        : null,
      h("div", { style: { whiteSpace: "pre-wrap", fontSize: "0.85rem", lineHeight: 1.5 } },
        m.content)
    );
  }

  // The /x/ai page — a continuable chat with the data navigator.
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

    function send() {
      var text = input.trim();
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
          if (data.error) {
            setMessages(next.concat([
              { role: "error", content: data.error, steps: data.steps },
            ]));
          } else {
            setMessages(next.concat([
              { role: "assistant", content: data.reply || "(no reply)", steps: data.steps },
            ]));
          }
        })
        .catch(function (err) {
          setMessages(next.concat([{ role: "error", content: String(err) }]));
        })
        .then(function () { setBusy(false); });
    }

    function onKeyDown(e) {
      if (e.key === "Enter" && !e.shiftKey) { e.preventDefault(); send(); }
    }

    return h("div", {
        style: {
          display: "flex", flexDirection: "column",
          height: "calc(100vh - 9rem)", maxWidth: "52rem", margin: "0 auto",
        },
      },
      h("h1", { style: { fontWeight: "bold", marginBottom: "0.2rem" } },
        "✨ AI Data Navigator"),
      h("p", { style: { fontSize: "0.8rem", opacity: 0.6, marginBottom: "0.75rem" } },
        "Ask about your data — the assistant inspects schemas and runs read-only " +
          "queries to answer. The conversation is continuable."),
      h("div", {
          style: {
            flex: 1, overflowY: "auto", display: "flex", flexDirection: "column",
            gap: "0.6rem", paddingRight: "0.25rem",
          },
        },
        messages.length === 0
          ? h("div", { style: { opacity: 0.5, fontSize: "0.85rem", padding: "1rem" } },
              "Try: “what tables do I have?” · “describe default.bronze.fr_orders” · " +
                "“how many rows are in default.bronze.fr_orders?”")
          : messages.map(function (m, i) { return h(Message, { key: i, m: m }); }),
        busy
          ? h("div", { style: { opacity: 0.6, fontSize: "0.85rem", padding: "0.5rem" } },
              "thinking…")
          : null,
        h("div", { ref: endRef })
      ),
      h("div", { style: { display: "flex", gap: "0.5rem", marginTop: "0.75rem" } },
        h("textarea", {
          value: input,
          onChange: function (e) { setInput(e.target.value); },
          onKeyDown: onKeyDown,
          rows: 2,
          placeholder: "Ask about your data…  (Enter to send, Shift+Enter for newline)",
          className: "brutal-card",
          style: {
            flex: 1, padding: "0.5rem", resize: "none", fontFamily: "inherit",
            fontSize: "0.85rem", background: "transparent", color: "inherit",
          },
        }),
        h("button", {
          onClick: send,
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
