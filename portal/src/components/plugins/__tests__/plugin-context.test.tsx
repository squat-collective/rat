// @vitest-environment jsdom
import React from "react";
import { describe, it, expect } from "vitest";
import { renderHook } from "@testing-library/react";
import { usePluginRegistry, PluginRegistryProvider } from "../plugin-context";
import type { PluginRegistry } from "../plugin-context";

describe("usePluginRegistry", () => {
  it("returns null outside provider", () => {
    const { result } = renderHook(() => usePluginRegistry());
    expect(result.current).toBeNull();
  });

  it("returns registry from provider", () => {
    const registry: PluginRegistry = {
      "sidebar-user": [() => null],
    };
    const wrapper = ({ children }: { children: React.ReactNode }) => (
      <PluginRegistryProvider registry={registry}>
        {children}
      </PluginRegistryProvider>
    );
    const { result } = renderHook(() => usePluginRegistry(), { wrapper });
    expect(result.current).toBe(registry);
  });

  it("accepts empty registry", () => {
    const registry: PluginRegistry = {};
    const wrapper = ({ children }: { children: React.ReactNode }) => (
      <PluginRegistryProvider registry={registry}>
        {children}
      </PluginRegistryProvider>
    );
    const { result } = renderHook(() => usePluginRegistry(), { wrapper });
    expect(result.current).toEqual({});
  });
});
