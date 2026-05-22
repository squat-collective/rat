/*
 * rat-plugin-event-notifier — portal UI bundle (Layer 3).
 *
 * Hand-written and build-free on purpose: an example portal plugin is just a
 * script that self-registers with the portal via the v3 plugin contract.
 *
 * The portal:
 *   1. exposes React on `window.React` and a registration hook on
 *      `window.__RAT_REGISTER_PLUGIN` BEFORE loading plugin bundles;
 *   2. injects this file as <script src="/api/v1/plugins/event-notifier/ui/bundle.js">
 *      (ratd reverse-proxies it from the plugin container).
 *
 * This bundle registers a dashboard widget, a sidebar nav item, and the page
 * that nav item links to. Real plugins typically build their bundle with a
 * bundler (React/ReactDOM as externals) — plain JS is used here for clarity.
 */
(function () {
  "use strict";

  var React = window.React;
  if (!React || typeof window.__RAT_REGISTER_PLUGIN !== "function") {
    console.warn("[event-notifier] RAT plugin host not available — skipping");
    return;
  }
  var h = React.createElement;

  // Component rendered into the `dashboard-widgets` slot.
  function EventNotifierWidget() {
    return h(
      "div",
      { className: "brutal-card", style: { padding: "1rem" } },
      h("div", { style: { fontWeight: "bold" } }, "📢 Event Notifier"),
      h(
        "div",
        { style: { fontSize: "0.85rem", opacity: 0.7, marginTop: "0.25rem" } },
        "Listening for run_completed and quality_failed events."
      )
    );
  }

  // Full page rendered at /x/event-notifier (the nav item target).
  function EventNotifierPage() {
    return h(
      "div",
      { className: "brutal-card", style: { padding: "1.5rem" } },
      h("h1", { style: { fontWeight: "bold" } }, "Event Notifier"),
      h(
        "p",
        { style: { marginTop: "0.5rem" } },
        "Provided by the rat-plugin-event-notifier platform plugin. Recent " +
          "events are available from its API route at " +
          "/api/v1/x/event-notifier/events."
      )
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
