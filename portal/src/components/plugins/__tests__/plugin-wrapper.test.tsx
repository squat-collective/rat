// @vitest-environment jsdom
import React from "react";
import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, cleanup, waitFor } from "@testing-library/react";
import { PluginWrapper } from "../plugin-wrapper";
import { usePluginRegistry } from "../plugin-context";

// Mock the plugin-loader module.
vi.mock("../plugin-loader", () => ({
  loadPlugins: vi.fn(),
}));

import { loadPlugins } from "../plugin-loader";
const mockLoadPlugins = vi.mocked(loadPlugins);

beforeEach(() => {
  cleanup();
  vi.clearAllMocks();
});

describe("PluginWrapper", () => {
  it("renders children directly when loadPlugins resolves empty registry", async () => {
    mockLoadPlugins.mockResolvedValue({ slots: {}, navItems: [], routes: [] });

    render(
      <PluginWrapper nonce="test-nonce">
        <div>App Content</div>
      </PluginWrapper>,
    );

    expect(screen.getByText("App Content")).toBeDefined();
  });

  it("renders children when loadPlugins rejects", async () => {
    mockLoadPlugins.mockRejectedValue(new Error("fetch failed"));

    render(
      <PluginWrapper nonce="test-nonce">
        <div>Still Visible</div>
      </PluginWrapper>,
    );

    // Children still render even on error.
    expect(screen.getByText("Still Visible")).toBeDefined();
  });

  it("provides registry to children after plugins load", async () => {
    function TestSlotConsumer() {
      const reg = usePluginRegistry();
      return <div>{reg ? `slots: ${Object.keys(reg.slots).length}` : "no registry"}</div>;
    }

    mockLoadPlugins.mockResolvedValue({
      slots: { "test-slot": [] },
      navItems: [{ label: "Test", icon: "puzzle", href: "/test", priority: 1 }],
      routes: [],
    });

    render(
      <PluginWrapper nonce="test-nonce">
        <TestSlotConsumer />
      </PluginWrapper>,
    );

    await waitFor(() => {
      expect(screen.getByText("slots: 1")).toBeDefined();
    });
  });
});
