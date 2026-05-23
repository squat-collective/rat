"use client";

import { useEffect } from "react";
import React from "react";
import ReactDOM from "react-dom";
import { mutate as globalMutate } from "swr";
import { useRouter } from "next/navigation";

/**
 * Exposes React, ReactDOM and a cache-invalidation helper on `window` so
 * plugin bundles can:
 *   - render with the portal's React (no duplicate copies)
 *   - tell the portal to refresh its caches after a direct fetch mutation
 *
 * Plugins call:
 *   window.__RAT_INVALIDATE()                       // refresh everything
 *   window.__RAT_INVALIDATE("table-")               // SWR prefix match
 *   window.__RAT_INVALIDATE((k) => k === "plugins") // SWR predicate match
 *
 * Every call also runs router.refresh() — many RAT list pages (pipelines,
 * explorer, settings) are Server Components fetched via serverApi and held
 * in Next.js's router cache, where SWR's mutate() can't reach.
 */
export function ReactGlobals() {
  const router = useRouter();
  useEffect(() => {
    const w = window as unknown as Record<string, unknown>;
    w.React = React;
    w.ReactDOM = ReactDOM;
    w.__RAT_INVALIDATE = (
      filter?: string | ((key: unknown) => boolean),
    ) => {
      const matcher =
        filter === undefined
          ? () => true
          : typeof filter === "string"
            ? (k: unknown) => typeof k === "string" && k.startsWith(filter)
            : filter;
      router.refresh();
      return globalMutate(matcher, undefined, { revalidate: true });
    };
  }, [router]);
  return null;
}
