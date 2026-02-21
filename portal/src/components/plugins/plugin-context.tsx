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

/** Props passed to a plugin route component. */
export type PluginRouteProps = {
  path: string[]; // URL segments after /x/ (e.g. ["my-plugin", "settings"])
};

/** A full-page route registered by a plugin. */
export type PluginRoute = {
  path: string; // e.g. "/x/my-plugin"
  component: React.ComponentType<PluginRouteProps>;
};

/** Merged registry from all loaded plugins. */
export type PluginRegistry = {
  slots: Record<string, SlotComponent<any>[]>;
  navItems: PluginNavItem[];
  routes: PluginRoute[];
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
