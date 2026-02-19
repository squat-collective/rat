// @vitest-environment jsdom
import React from "react";
import { describe, it, expect } from "vitest";
import { render, screen } from "@testing-library/react";
import { PluginSlot } from "../plugin-slot";
import { PluginRegistryProvider } from "../plugin-context";
import type { PluginRegistry, SlotComponent } from "../plugin-context";

describe("PluginSlot", () => {
  it("renders nothing when no PluginRegistryProvider wraps it", () => {
    const { container } = render(<PluginSlot name="sidebar-user" />);
    expect(container.innerHTML).toBe("");
  });

  it("renders fallback when no provider present", () => {
    render(
      <PluginSlot name="sidebar-user" fallback={<span>fallback</span>} />,
    );
    expect(screen.getByText("fallback")).toBeDefined();
  });

  it("renders nothing when slot has no registered components", () => {
    const registry: PluginRegistry = {};
    const { container } = render(
      <PluginRegistryProvider registry={registry}>
        <PluginSlot name="sidebar-user" />
      </PluginRegistryProvider>,
    );
    expect(container.innerHTML).toBe("");
  });

  it("renders registered component for the slot", () => {
    const UserMenu: SlotComponent = () => <div>User Menu</div>;
    const registry: PluginRegistry = {
      "sidebar-user": [UserMenu],
    };
    render(
      <PluginRegistryProvider registry={registry}>
        <PluginSlot name="sidebar-user" />
      </PluginRegistryProvider>,
    );
    expect(screen.getByText("User Menu")).toBeDefined();
  });

  it("renders multiple components for the same slot", () => {
    const CompA: SlotComponent = () => <div>Component A</div>;
    const CompB: SlotComponent = () => <div>Component B</div>;
    const registry: PluginRegistry = {
      "sidebar-user": [CompA, CompB],
    };
    render(
      <PluginRegistryProvider registry={registry}>
        <PluginSlot name="sidebar-user" />
      </PluginRegistryProvider>,
    );
    expect(screen.getByText("Component A")).toBeDefined();
    expect(screen.getByText("Component B")).toBeDefined();
  });

  it("passes extra props to slot components", () => {
    const NavExtra: SlotComponent<{ collapsed: boolean }> = ({ collapsed }) => (
      <div>{collapsed ? "collapsed" : "expanded"}</div>
    );
    const registry: PluginRegistry = {
      "sidebar-nav-extra": [NavExtra],
    };
    render(
      <PluginRegistryProvider registry={registry}>
        <PluginSlot name="sidebar-nav-extra" collapsed={true} />
      </PluginRegistryProvider>,
    );
    expect(screen.getByText("collapsed")).toBeDefined();
  });

  it("renders nothing for unknown slot name", () => {
    const registry: PluginRegistry = {
      "sidebar-user": [() => <div>User</div>],
    };
    const { container } = render(
      <PluginRegistryProvider registry={registry}>
        <PluginSlot name="main-header" />
      </PluginRegistryProvider>,
    );
    expect(container.innerHTML).toBe("");
  });
});
