"use client";

import { useEffect } from "react";
import React from "react";
import ReactDOM from "react-dom";
import { mutate as globalMutate } from "swr";

/**
 * Exposes React, ReactDOM and a cache-invalidation helper on `window` so
 * plugin bundles can:
 *   - render with the portal's React (no duplicate copies)
 *   - tell the portal to refresh its SWR caches after a direct fetch
 *
 * Plugins call:
 *   window.__RAT_INVALIDATE()                       // revalidate every cached key
 *   window.__RAT_INVALIDATE("table-")               // prefix match
 *   window.__RAT_INVALIDATE((k) => k === "plugins") // predicate match
 *
 * This is the cross-plugin equivalent of SWR's mutate(): required because
 * plugin bundles fetch through plain HTTP and the portal's hooks otherwise
 * have no way to know their cache went stale.
 */
export function ReactGlobals() {
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
      return globalMutate(matcher, undefined, { revalidate: true });
    };
  }, []);
  return null;
}
