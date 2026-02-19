"use client";

import { useState, useEffect } from "react";
import { PluginRegistryProvider } from "./plugin-context";
import type { PluginRegistry } from "./plugin-context";

/**
 * Set NEXT_PUBLIC_PLUGIN_PACKAGE to the name of an npm package that exports
 * a `loadRegistry(): Promise<PluginRegistry>` function. When configured, the
 * wrapper dynamically imports the package at mount and provides the registry
 * to all PluginSlot components in the tree.
 *
 * When unset, children render directly with no overhead.
 */
const PLUGIN_PACKAGE = process.env.NEXT_PUBLIC_PLUGIN_PACKAGE;

export function PluginWrapper({ children }: { children: React.ReactNode }) {
  const [registry, setRegistry] = useState<PluginRegistry | null>(null);

  useEffect(() => {
    if (!PLUGIN_PACKAGE) return;

    let cancelled = false;

    import(/* webpackIgnore: true */ PLUGIN_PACKAGE)
      .then((mod) => {
        if (cancelled) return;
        if (typeof mod.loadRegistry === "function") {
          return mod.loadRegistry();
        }
        return null;
      })
      .then((reg: PluginRegistry | null | undefined) => {
        if (cancelled || !reg) return;
        setRegistry(reg);
      })
      .catch((err) => {
        console.error("[PluginWrapper] Failed to load plugin package:", err);
      });

    return () => {
      cancelled = true;
    };
  }, []);

  if (PLUGIN_PACKAGE && registry) {
    return (
      <PluginRegistryProvider registry={registry}>
        {children}
      </PluginRegistryProvider>
    );
  }

  return <>{children}</>;
}
