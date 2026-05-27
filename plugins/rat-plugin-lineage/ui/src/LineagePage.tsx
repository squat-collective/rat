// Page-level component for /x/lineage. Owns the namespace filter, the
// data fetch, and renders either a loading / empty / error state or
// the LineageDag visualization.
import { useEffect, useState } from "react";
import { LineageDag } from "./LineageDag";
import type { LineageGraph, Namespace } from "./types";

// API base — the bundle is served from ratd's plugin proxy
// (/api/v1/plugins/lineage/ui/bundle.js), so the base of the script
// src is the portal/ratd origin. From there our endpoints live under
// /api/v1/x/lineage/. Falls back to the document origin.
function apiBase(): string {
  const s = document.querySelector(
    'script[src*="/plugins/lineage/ui/bundle.js"]',
  ) as HTMLScriptElement | null;
  if (s && s.src && s.src.indexOf("/api/v1/") !== -1) {
    return s.src.slice(0, s.src.indexOf("/api/v1/"));
  }
  return window.location.origin;
}

async function fetchGraph(namespace?: string): Promise<LineageGraph> {
  const base = apiBase();
  const url =
    base +
    "/api/v1/x/lineage/graph" +
    (namespace ? "?namespace=" + encodeURIComponent(namespace) : "");
  const res = await fetch(url);
  if (!res.ok) throw new Error("HTTP " + res.status + ": " + (await res.text()));
  return res.json();
}

async function fetchNamespaces(): Promise<Namespace[]> {
  const base = apiBase();
  const res = await fetch(base + "/api/v1/namespaces");
  if (!res.ok) return [];
  const data = (await res.json()) as { namespaces?: Namespace[] };
  return data.namespaces ?? [];
}

export function LineageApp() {
  const [namespace, setNamespace] = useState<string>("");
  const [graph, setGraph] = useState<LineageGraph | null>(null);
  const [namespaces, setNamespaces] = useState<Namespace[]>([]);
  const [loading, setLoading] = useState<boolean>(true);
  const [error, setError] = useState<string | null>(null);

  // Initial + interval refresh, mirrors the old useLineage's 30s tick.
  useEffect(() => {
    let cancelled = false;
    let timer: number | null = null;
    async function load() {
      try {
        setError(null);
        const g = await fetchGraph(namespace || undefined);
        if (!cancelled) {
          setGraph(g);
          setLoading(false);
        }
      } catch (e) {
        if (!cancelled) {
          setError((e as Error).message);
          setLoading(false);
        }
      }
    }
    setLoading(true);
    load();
    timer = window.setInterval(load, 30000);
    return () => {
      cancelled = true;
      if (timer) window.clearInterval(timer);
    };
  }, [namespace]);

  useEffect(() => {
    fetchNamespaces().then(setNamespaces).catch(() => {});
  }, []);

  const headerColor = "hsl(var(--foreground, 0 0% 90%))";
  const mutedColor = "hsl(var(--muted-foreground, 0 0% 50%))";
  const borderColor = "hsl(var(--border, 0 0% 16%))";
  const bgColor = "hsl(var(--background, 0 0% 4%))";

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 12 }}>
      {/* Header */}
      <div
        style={{
          display: "flex",
          alignItems: "center",
          justifyContent: "space-between",
        }}
      >
        <div>
          <h1
            style={{
              margin: 0,
              fontSize: 14,
              fontWeight: 700,
              letterSpacing: 1,
              color: headerColor,
            }}
          >
            LINEAGE
          </h1>
          <p style={{ margin: "2px 0 0", fontSize: 10, color: mutedColor }}>
            Pipeline dependency graph
          </p>
        </div>

        <select
          value={namespace}
          onChange={(e) => setNamespace(e.target.value)}
          style={{
            padding: "4px 8px",
            background: bgColor,
            color: headerColor,
            border: "1px solid " + borderColor,
            fontSize: 12,
            fontFamily: "inherit",
            minWidth: 180,
          }}
        >
          <option value="">All namespaces</option>
          {namespaces.map((n) => (
            <option key={n.name} value={n.name}>
              {n.name}
            </option>
          ))}
        </select>
      </div>

      {error && (
        <div
          style={{
            padding: 10,
            background: "rgba(239,68,68,0.10)",
            border: "1px solid hsl(0, 62%, 35%)",
            color: "hsl(0, 62%, 60%)",
            fontSize: 12,
          }}
        >
          Failed to load lineage: {error}
        </div>
      )}

      {loading && !graph && (
        <div style={{ fontSize: 12, color: mutedColor, padding: 16 }}>
          Building lineage…
        </div>
      )}

      {graph && graph.nodes.length > 0 ? (
        // key forces a full remount when the namespace changes — defence
        // in depth against React Flow's internal state holding the old
        // graph even after we setNodes/setEdges in LineageDag.
        <LineageDag key={namespace || "__all__"} graph={graph} />
      ) : (
        !error &&
        !loading && (
          <div
            style={{
              border: "2px solid " + borderColor,
              padding: 32,
              textAlign: "center",
              fontSize: 12,
              color: mutedColor,
            }}
          >
            No pipelines found. Create your first pipeline to see the lineage graph.
          </div>
        )
      )}
    </div>
  );
}
