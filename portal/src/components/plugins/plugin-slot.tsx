"use client";

import { usePluginRegistry } from "./plugin-context";

interface PluginSlotProps {
  /** The slot name to render components for. */
  name: string;
  /** Optional fallback rendered when no provider is present. */
  fallback?: React.ReactNode;
  /** Extra props passed through to each slot component. */
  [key: string]: unknown;
}

export function PluginSlot({ name, fallback, ...props }: PluginSlotProps) {
  const registry = usePluginRegistry();

  if (!registry) {
    return fallback ? <>{fallback}</> : null;
  }

  const components = registry[name];
  if (!components || components.length === 0) {
    return null;
  }

  return (
    <>
      {components.map((Component, i) => (
        <Component key={i} {...props} />
      ))}
    </>
  );
}
