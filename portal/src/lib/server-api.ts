/**
 * Server-side API fetch helper.
 *
 * Uses API_URL (internal Docker network) for server components,
 * falls back to NEXT_PUBLIC_API_URL for dev/build.
 *
 * TODO(P10-58): Add runtime type validation on API responses using Zod schemas.
 * Once zod is added as a dependency, define schemas for PipelineListResponse,
 * RunListResponse, TableListResponse, and FeaturesResponse, then validate in
 * apiFetch() before returning.
 */
import type {
  PipelineListResponse,
  RunListResponse,
  TableListResponse,
  FeaturesResponse,
} from "@squat-collective/rat-client";

export type { PipelineListResponse, RunListResponse, TableListResponse, FeaturesResponse };

const SERVER_API_URL =
  process.env.API_URL ?? process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:8080";

/** Timeout in milliseconds for server-side API fetch calls. */
const FETCH_TIMEOUT_MS = 10_000;

/**
 * Default revalidation period in seconds for server-side data fetching.
 *
 * Set to 0 — every navigation re-fetches. RAT is an interactive admin UI
 * for a self-hosted, local backend (ratd in the same Docker network), so
 * the cost of always-fresh data is tiny but the cost of staleness is real:
 * a previously-cached list would keep showing a deleted pipeline (or miss
 * a just-created one) until the cache expired.
 *
 * router.refresh() alone is NOT enough — it invalidates the client router
 * cache, but a Next.js Server Component still served from the fetch data
 * cache returns the same stale response. Setting revalidate: 0 here is
 * equivalent to cache: "no-store" — the fetch result is never cached.
 */
const DEFAULT_REVALIDATE_SECONDS = 0;

/** Response shape from GET /api/v1/me. */
export interface MeResponse {
  user_id: string;
  email: string;
  display_name: string;
  roles: string[];
}

async function apiFetch<T>(
  path: string,
  revalidateSeconds: number = DEFAULT_REVALIDATE_SECONDS,
  accessToken?: string,
): Promise<T> {
  const controller = new AbortController();
  const timeoutId = setTimeout(() => controller.abort(), FETCH_TIMEOUT_MS);
  try {
    const headers: Record<string, string> = {};
    if (accessToken) {
      headers["Authorization"] = `Bearer ${accessToken}`;
    }
    const res = await fetch(`${SERVER_API_URL}${path}`, {
      headers,
      next: { revalidate: revalidateSeconds },
      signal: controller.signal,
    });
    if (!res.ok) {
      throw new Error(`API ${path}: ${res.status} ${res.statusText}`);
    }
    return res.json() as Promise<T>;
  } catch (err) {
    if (err instanceof DOMException && err.name === "AbortError") {
      throw new Error(`API ${path}: request timed out after ${FETCH_TIMEOUT_MS}ms`);
    }
    throw err;
  } finally {
    clearTimeout(timeoutId);
  }
}

/** Create an authenticated server API client. */
export function createAuthServerApi(accessToken: string) {
  return {
    pipelines: {
      list: () => apiFetch<PipelineListResponse>("/api/v1/pipelines", DEFAULT_REVALIDATE_SECONDS, accessToken),
    },
    runs: {
      list: () => apiFetch<RunListResponse>("/api/v1/runs", 5, accessToken),
    },
    tables: {
      list: () => apiFetch<TableListResponse>("/api/v1/tables", DEFAULT_REVALIDATE_SECONDS, accessToken),
    },
    features: () => apiFetch<FeaturesResponse>("/api/v1/features", 60, accessToken),
    me: () => apiFetch<MeResponse>("/api/v1/me", 0, accessToken),
  };
}

export const serverApi = {
  pipelines: {
    list: () => apiFetch<PipelineListResponse>("/api/v1/pipelines"),
  },
  runs: {
    // Runs change frequently — use shorter revalidation (5s).
    list: () => apiFetch<RunListResponse>("/api/v1/runs", 5),
  },
  tables: {
    list: () => apiFetch<TableListResponse>("/api/v1/tables"),
  },
  // Features rarely change (plugin load is at startup) — cache for 60s.
  features: () => apiFetch<FeaturesResponse>("/api/v1/features", 60),
};
