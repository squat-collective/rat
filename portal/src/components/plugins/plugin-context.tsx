"use client";

import { createContext, useContext } from "react";

/** A component that can be rendered in a plugin slot. */
export type SlotComponent<P = Record<string, unknown>> = React.ComponentType<P>;

/** Maps slot names to arrays of components registered for that slot. */
export type PluginRegistry = Record<string, SlotComponent<any>[]>;

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
