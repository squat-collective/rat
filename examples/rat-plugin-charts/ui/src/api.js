// REST client for the charts plugin. ratd proxies the plugin's API under
// /api/v1/x/charts; the AI-analysis component also calls the AI plugin under
// /api/v1/x/ai.

function apiBase() {
  // The bundle is served at {origin}/api/v1/plugins/charts/ui/bundle.js —
  // recover {origin} from this script's own src.
  const s = document.querySelector('script[src*="/plugins/charts/ui/bundle.js"]');
  if (s && s.src && s.src.indexOf("/api/v1/") !== -1) {
    return s.src.slice(0, s.src.indexOf("/api/v1/"));
  }
  return window.location.origin;
}

const ORIGIN = apiBase();
export const API_ROOT = ORIGIN + "/api/v1/x/charts";
const AI_ROOT = ORIGIN + "/api/v1/x/ai";

async function req(method, url, body) {
  const opts = { method, headers: {} };
  if (body !== undefined) {
    opts.headers["Content-Type"] = "application/json";
    opts.body = JSON.stringify(body);
  }
  const res = await fetch(url, opts);
  if (res.status === 204) return null;

  const text = await res.text();
  let data = null;
  try {
    data = text ? JSON.parse(text) : null;
  } catch (e) {
    data = { error: text };
  }
  if (!res.ok) {
    throw new Error((data && data.error) || "request failed: " + res.status);
  }
  return data;
}

export const api = {
  // Dashboards
  listDashboards: () => req("GET", API_ROOT + "/dashboards"),
  createDashboard: (title) => req("POST", API_ROOT + "/dashboards", { title: title }),
  getDashboard: (id) => req("GET", API_ROOT + "/dashboards/" + id),
  updateDashboard: (id, patch) => req("PUT", API_ROOT + "/dashboards/" + id, patch),
  deleteDashboard: (id) => req("DELETE", API_ROOT + "/dashboards/" + id),
  addComponent: (id, component) =>
    req("POST", API_ROOT + "/dashboards/" + id + "/components", component),

  // Run a component's SQL (live data).
  query: (sql) => req("POST", API_ROOT + "/query", { sql: sql }),

  // AI plugin — the AI-analysis component asks it for an insight.
  analyze: (prompt, data) => req("POST", AI_ROOT + "/analyze", { prompt: prompt, data: data }),
};
