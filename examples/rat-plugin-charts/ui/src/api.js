// REST client for the charts plugin. ratd proxies the plugin's API under
// /api/v1/x/charts, so the UI talks to the same origin it was served from.

function apiBase() {
  // The bundle is served at {origin}/api/v1/plugins/charts/ui/bundle.js —
  // recover {origin} from this script's own src.
  const s = document.querySelector('script[src*="/plugins/charts/ui/bundle.js"]');
  if (s && s.src && s.src.indexOf("/api/v1/") !== -1) {
    return s.src.slice(0, s.src.indexOf("/api/v1/"));
  }
  return window.location.origin;
}

export const API_ROOT = apiBase() + "/api/v1/x/charts";

async function req(method, path, body) {
  const opts = { method, headers: {} };
  if (body !== undefined) {
    opts.headers["Content-Type"] = "application/json";
    opts.body = JSON.stringify(body);
  }
  const res = await fetch(API_ROOT + path, opts);
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
  // Charts
  listCharts: () => req("GET", "/charts"),
  createChart: (c) => req("POST", "/charts", c),
  chartData: (id) => req("GET", "/charts/" + id + "/data"),
  deleteChart: (id) => req("DELETE", "/charts/" + id),
  preview: (sql) => req("POST", "/preview", { sql: sql }),

  // Dashboards
  listDashboards: () => req("GET", "/dashboards"),
  createDashboard: (d) => req("POST", "/dashboards", d),
  getDashboard: (id) => req("GET", "/dashboards/" + id),
  updateDashboard: (id, patch) => req("PATCH", "/dashboards/" + id, patch),
  deleteDashboard: (id) => req("DELETE", "/dashboards/" + id),

  // Reports
  listReports: () => req("GET", "/reports"),
  createReport: (r) => req("POST", "/reports", r),
  getReport: (id) => req("GET", "/reports/" + id),
  deleteReport: (id) => req("DELETE", "/reports/" + id),
};
