/*
 * rat-plugin-event-notifier — portal UI bundle (Layer 3).
 *
 * Hand-written and build-free on purpose: an example portal plugin is just a
 * script that self-registers with the portal via the v3 plugin contract.
 *
 * The portal exposes React on `window.React` and a registration hook on
 * `window.__RAT_REGISTER_PLUGIN`, then injects this file as
 * <script src="/api/v1/plugins/event-notifier/ui/bundle.js"> (ratd proxies it
 * from this container). This bundle registers a dashboard widget, a sidebar
 * nav item, and the page that nav item links to.
 */
(function () {
  "use strict";

  var React = window.React;
  if (!React || typeof window.__RAT_REGISTER_PLUGIN !== "function") {
    console.warn("[event-notifier] RAT plugin host not available — skipping");
    return;
  }
  var h = React.createElement;

  // Discover the API base from this bundle's own <script src>, which the
  // portal set to "<api>/api/v1/plugins/event-notifier/ui/bundle.js".
  function apiBase() {
    var s = document.querySelector(
      'script[src*="/plugins/event-notifier/ui/bundle.js"]'
    );
    if (s && s.src && s.src.indexOf("/api/v1/") !== -1) {
      return s.src.slice(0, s.src.indexOf("/api/v1/"));
    }
    return window.location.origin;
  }
  var EVENTS_URL = apiBase() + "/api/v1/x/event-notifier/events";

  // Fetch the events the plugin's /events route exposes (proxied by ratd).
  function useEvents() {
    var st = React.useState({ loading: true, events: [], error: null });
    var state = st[0], set = st[1];
    React.useEffect(function () {
      var alive = true;
      fetch(EVENTS_URL)
        .then(function (r) {
          if (!r.ok) throw new Error("HTTP " + r.status);
          return r.json();
        })
        .then(function (events) {
          if (alive) set({ loading: false, events: events || [], error: null });
        })
        .catch(function (err) {
          if (alive) set({ loading: false, events: [], error: String(err) });
        });
      return function () { alive = false; };
    }, []);
    return state;
  }

  var thStyle = { textAlign: "left", padding: "0.4rem 0.5rem", opacity: 0.6 };
  var tdStyle = { padding: "0.4rem 0.5rem", fontFamily: "monospace", fontSize: "0.8rem" };

  // Component for the `dashboard-widgets` slot — a clickable summary card.
  function EventNotifierWidget() {
    var s = useEvents();
    var count = s.loading ? "…" : s.error ? "—" : String(s.events.length);
    return h("a", {
        href: "/x/event-notifier",
        className: "brutal-card",
        style: { display: "block", padding: "1rem", textDecoration: "none" },
      },
      h("div", { style: { fontWeight: "bold" } }, "📢 Event Notifier"),
      h("div", { style: { fontSize: "1.6rem", fontWeight: "bold", marginTop: "0.25rem" } }, count),
      h("div", { style: { fontSize: "0.8rem", opacity: 0.7 } },
        "platform events captured — click to view")
    );
  }

  // Full page rendered at /x/event-notifier (the nav item / widget target).
  function EventNotifierPage() {
    var s = useEvents();
    var body;
    if (s.loading) {
      body = h("p", { style: { opacity: 0.7 } }, "Loading events…");
    } else if (s.error) {
      body = h("p", { style: { opacity: 0.7 } }, "Could not load events: " + s.error);
    } else if (s.events.length === 0) {
      body = h("p", { style: { opacity: 0.7 } },
        "No events captured yet. Trigger a pipeline run, then reload this page.");
    } else {
      body = h("table", { style: { width: "100%", borderCollapse: "collapse" } },
        h("thead", null, h("tr", null,
          h("th", { style: thStyle }, "Type"),
          h("th", { style: thStyle }, "Timestamp"),
          h("th", { style: thStyle }, "Event ID"))),
        h("tbody", null, s.events.slice().reverse().map(function (ev, i) {
          return h("tr", { key: i },
            h("td", { style: tdStyle }, ev.type),
            h("td", { style: tdStyle }, ev.timestamp),
            h("td", { style: tdStyle }, ev.id));
        }))
      );
    }
    return h("div", { className: "brutal-card", style: { padding: "1.5rem" } },
      h("h1", { style: { fontWeight: "bold", marginBottom: "0.25rem" } }, "📢 Event Notifier"),
      h("p", { style: { fontSize: "0.85rem", opacity: 0.6, marginBottom: "1rem" } },
        "Platform events delivered to this plugin via HandleEvent."),
      body
    );
  }

  window.__RAT_REGISTER_PLUGIN("event-notifier", {
    slots: { "dashboard-widgets": [EventNotifierWidget] },
    navItems: [
      { label: "Events", icon: "bell", href: "/x/event-notifier", priority: 50 },
    ],
    routes: [{ path: "/x/event-notifier", component: EventNotifierPage }],
  });
  console.info("[event-notifier] plugin registered with the portal");
})();
