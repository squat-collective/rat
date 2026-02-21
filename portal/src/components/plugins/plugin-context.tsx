"use client";

import { createContext, useContext } from "react";

/** A component that can be rendered in a plugin slot. */
export type SlotComponent<P = Record<string, unknown>> = React.ComponentType<P>;

/** A navigation item injected by a plugin into the sidebar. */
export type PluginNavItem = {
  label: string;
  icon: string; // lucide icon name
  href: string;
  priority: number;
};

/** Merged registry from all loaded plugins. */
export type PluginRegistry = {
  slots: Record<string, SlotComponent<any>[]>;
  navItems: PluginNavItem[];
};

const PluginContext = createContext<PluginRegistry | null>(null);

export function PluginRegistryProvider({
  registry,
  children,
}: {
  registry: PluginRegistry;
  children: React.ReactNode;
}) {
  return (
    <PluginContext.Provider value={registry}>{children}</PluginContext.Provider>
  );
}

export function usePluginRegistry(): PluginRegistry | null {
  return useContext(PluginContext);
}
