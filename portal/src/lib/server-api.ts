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
 * Uses Next.js time-based revalidation (ISR) instead of "no-store" to enable
 * caching of slow-changing data (pipelines, tables, features) while keeping
 * the portal responsive. Individual fetches can override this via the
 * revalidateSeconds parameter.
 *
 * Trade-off: data may be up to N seconds stale, but page loads are faster
 * because Next.js serves from cache and revalidates in the background.
 */
const DEFAULT_REVALIDATE_SECONDS = 10;

async function apiFetch<T>(
  path: string,
  revalidateSeconds: number = DEFAULT_REVALIDATE_SECONDS,
): Promise<T> {
  const controller = new AbortController();
  const timeoutId = setTimeout(() => controller.abort(), FETCH_TIMEOUT_MS);
  try {
    const res = await fetch(`${SERVER_API_URL}${path}`, {
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
