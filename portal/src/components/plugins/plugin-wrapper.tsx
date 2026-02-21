"use client";

import { useState, useEffect } from "react";
import { PluginRegistryProvider } from "./plugin-context";
import type { PluginRegistry } from "./plugin-context";
import { loadPlugins } from "./plugin-loader";

/**
 * Loads all enabled plugin bundles via the platform API and provides the
 * merged registry to all PluginSlot components in the tree.
 *
 * While plugins are loading (or if none are registered), children render
 * directly without a registry provider — PluginSlot gracefully handles null.
 */
export function PluginWrapper({
  nonce,
  children,
}: {
  nonce: string;
  children: React.ReactNode;
}) {
  const [registry, setRegistry] = useState<PluginRegistry | null>(null);

  useEffect(() => {
    let cancelled = false;

    loadPlugins(nonce)
      .then((reg) => {
        if (!cancelled) setRegistry(reg);
      })
      .catch((err) => {
        console.error("[PluginWrapper] Failed to load plugins:", err);
      });

    return () => {
      cancelled = true;
    };
  }, [nonce]);

  if (registry) {
    return (
      <PluginRegistryProvider registry={registry}>
        {children}
      </PluginRegistryProvider>
    );
  }

  return <>{children}</>;
}
