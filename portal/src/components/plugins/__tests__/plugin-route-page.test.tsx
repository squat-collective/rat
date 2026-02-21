// @vitest-environment jsdom
import React from "react";
import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, cleanup } from "@testing-library/react";
import { PluginRegistryProvider } from "../plugin-context";
import type { PluginRegistry, PluginRouteProps } from "../plugin-context";

// Mock next/navigation to control useParams.
const mockParams = vi.fn<() => { path: string[] }>();
vi.mock("next/navigation", () => ({
  useParams: () => mockParams(),
}));

// Import the page component after mocks are set up.
import PluginRoutePage from "@/app/x/[...path]/page";

beforeEach(() => {
  cleanup();
  vi.clearAllMocks();
});

describe("PluginRoutePage", () => {
  it("shows not-found when no registry is available", () => {
    mockParams.mockReturnValue({ path: ["my-plugin"] });

    render(<PluginRoutePage />);

    expect(screen.getByText("plugin route not found")).toBeDefined();
  });

  it("shows not-found when no route matches", () => {
    mockParams.mockReturnValue({ path: ["unknown-plugin"] });

    const registry: PluginRegistry = {
      slots: {},
      navItems: [],
      routes: [
        { path: "/x/my-plugin", component: () => <div>My Plugin</div> },
      ],
    };

    render(
      <PluginRegistryProvider registry={registry}>
        <PluginRoutePage />
      </PluginRegistryProvider>,
    );

    expect(screen.getByText("plugin route not found")).toBeDefined();
  });

  it("renders matched route component", () => {
    mockParams.mockReturnValue({ path: ["my-plugin"] });

    const MyPage = () => <div>My Plugin Page</div>;
    const registry: PluginRegistry = {
      slots: {},
      navItems: [],
      routes: [{ path: "/x/my-plugin", component: MyPage }],
    };

    render(
      <PluginRegistryProvider registry={registry}>
        <PluginRoutePage />
      </PluginRegistryProvider>,
    );

    expect(screen.getByText("My Plugin Page")).toBeDefined();
  });

  it("passes correct path segments to the component", () => {
    mockParams.mockReturnValue({ path: ["my-plugin", "settings", "advanced"] });

    const MyPage = ({ path }: PluginRouteProps) => (
      <div>path: {path.join("/")}</div>
    );
    const registry: PluginRegistry = {
      slots: {},
      navItems: [],
      routes: [{ path: "/x/my-plugin", component: MyPage }],
    };

    render(
      <PluginRegistryProvider registry={registry}>
        <PluginRoutePage />
      </PluginRegistryProvider>,
    );

    expect(screen.getByText("path: my-plugin/settings/advanced")).toBeDefined();
  });

  it("prefix matching works — registered /x/foo matches /x/foo/bar/baz", () => {
    mockParams.mockReturnValue({ path: ["foo", "bar", "baz"] });

    const FooPage = ({ path }: PluginRouteProps) => (
      <div>foo sub: {path.slice(1).join("/")}</div>
    );
    const registry: PluginRegistry = {
      slots: {},
      navItems: [],
      routes: [{ path: "/x/foo", component: FooPage }],
    };

    render(
      <PluginRegistryProvider registry={registry}>
        <PluginRoutePage />
      </PluginRegistryProvider>,
    );

    expect(screen.getByText("foo sub: bar/baz")).toBeDefined();
  });

  it("matches exact path without trailing segments", () => {
    mockParams.mockReturnValue({ path: ["monitoring"] });

    const MonitoringPage = () => <div>Monitoring Home</div>;
    const registry: PluginRegistry = {
      slots: {},
      navItems: [],
      routes: [{ path: "/x/monitoring", component: MonitoringPage }],
    };

    render(
      <PluginRegistryProvider registry={registry}>
        <PluginRoutePage />
      </PluginRegistryProvider>,
    );

    expect(screen.getByText("Monitoring Home")).toBeDefined();
  });

  it("does not match partial prefix — /x/foo does not match /x/foobar", () => {
    mockParams.mockReturnValue({ path: ["foobar"] });

    const FooPage = () => <div>Foo Page</div>;
    const registry: PluginRegistry = {
      slots: {},
      navItems: [],
      routes: [{ path: "/x/foo", component: FooPage }],
    };

    render(
      <PluginRegistryProvider registry={registry}>
        <PluginRoutePage />
      </PluginRegistryProvider>,
    );

    expect(screen.getByText("plugin route not found")).toBeDefined();
  });
});
