// @vitest-environment jsdom
import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { loadPlugins } from "../plugin-loader";

// Mock the api-client module to control PUBLIC_API_URL.
vi.mock("@/lib/api-client", () => ({
  PUBLIC_API_URL: "http://localhost:8080",
}));

beforeEach(() => {
  vi.restoreAllMocks();
  delete (window as any).__RAT_REGISTER_PLUGIN;
  // Clean up injected script tags.
  document.head.querySelectorAll("script[src*='bundle.js']").forEach((el) => el.remove());
});

afterEach(() => {
  delete (window as any).__RAT_REGISTER_PLUGIN;
});

describe("loadPlugins", () => {
  it("returns empty registry when no plugins have UI bundles", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(
      new Response(JSON.stringify([
        { name: "auth-keycloak", status: "enabled", descriptor: {} },
      ]), { status: 200 }),
    );

    const reg = await loadPlugins("test-nonce");

    expect(reg).toEqual({ slots: {}, navItems: [], routes: [] });
  });

  it("returns empty registry when fetch fails", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(
      new Response("", { status: 500 }),
    );

    const reg = await loadPlugins("test-nonce");

    expect(reg).toEqual({ slots: {}, navItems: [], routes: [] });
  });

  it("returns empty registry when fetch throws", async () => {
    vi.spyOn(globalThis, "fetch").mockRejectedValue(new Error("network error"));

    const reg = await loadPlugins("test-nonce");

    expect(reg).toEqual({ slots: {}, navItems: [], routes: [] });
  });

  it("sets up __RAT_REGISTER_PLUGIN on window", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(
      new Response(JSON.stringify([]), { status: 200 }),
    );

    await loadPlugins("test-nonce");

    expect(typeof window.__RAT_REGISTER_PLUGIN).toBe("function");
  });

  it("injects script tags for plugins with UI bundles", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(
      new Response(JSON.stringify([
        {
          name: "my-plugin",
          status: "enabled",
          descriptor: {
            ui: {
              bundle_url: "http://plugin:3000/bundle.js",
              bundle_hash: "sha256-AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
            },
          },
        },
      ]), { status: 200 }),
    );

    // Mock script loading: simulate onload firing immediately.
    const origCreateElement = document.createElement.bind(document);
    vi.spyOn(document, "createElement").mockImplementation((tag: string) => {
      const el = origCreateElement(tag);
      if (tag === "script") {
        // Simulate async script load.
        setTimeout(() => {
          el.dispatchEvent(new Event("load"));
        }, 0);
      }
      return el;
    });

    await loadPlugins("test-nonce");

    const scripts = document.head.querySelectorAll("script[src*='bundle.js']");
    expect(scripts.length).toBe(1);
    expect((scripts[0] as HTMLScriptElement).src).toBe(
      "http://localhost:8080/api/v1/plugins/my-plugin/ui/bundle.js",
    );
    expect((scripts[0] as HTMLScriptElement).nonce).toBe("test-nonce");
    // The loader tags every injected <script> so a later loadPlugins() call
    // can clean it up before re-injecting.
    expect((scripts[0] as HTMLScriptElement).dataset.ratPlugin).toBe("my-plugin");
  });

  it("removes_old_plugin_script_tags_before_re-inject", async () => {
    // Two enabled plugins, both with bundles. Use mockImplementation so each
    // fetch() call returns a fresh Response — Response bodies are single-use
    // streams, so mockResolvedValue (which returns the SAME instance) breaks
    // on the second call.
    const pluginListJson = JSON.stringify([
      {
        name: "plugin-a",
        status: "enabled",
        descriptor: {
          ui: {
            bundle_url: "http://a:3000/bundle.js",
            bundle_hash: "sha256-AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
          },
        },
      },
      {
        name: "plugin-b",
        status: "enabled",
        descriptor: {
          ui: {
            bundle_url: "http://b:3000/bundle.js",
            bundle_hash: "sha256-BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB=",
          },
        },
      },
    ]);
    vi.spyOn(globalThis, "fetch").mockImplementation(
      async () => new Response(pluginListJson, { status: 200 }),
    );

    const origCreateElement = document.createElement.bind(document);
    vi.spyOn(document, "createElement").mockImplementation((tag: string) => {
      const el = origCreateElement(tag);
      if (tag === "script") {
        setTimeout(() => el.dispatchEvent(new Event("load")), 0);
      }
      return el;
    });

    // First load — should inject two tags.
    await loadPlugins("nonce-1");
    expect(
      document.head.querySelectorAll("script[data-rat-plugin]").length,
    ).toBe(2);

    // Second load (simulates navigation: nonce changes per request) — must
    // remove the old tags and inject fresh ones. No accumulation.
    await loadPlugins("nonce-2");
    const allTags = document.head.querySelectorAll("script[data-rat-plugin]");
    expect(allTags.length).toBe(2);
    expect(
      document.head.querySelectorAll('script[data-rat-plugin="plugin-a"]')
        .length,
    ).toBe(1);
    expect(
      document.head.querySelectorAll('script[data-rat-plugin="plugin-b"]')
        .length,
    ).toBe(1);
    // The freshly injected tags carry the new nonce.
    expect((allTags[0] as HTMLScriptElement).nonce).toBe("nonce-2");
    expect((allTags[1] as HTMLScriptElement).nonce).toBe("nonce-2");
  });

  it("merges registrations from multiple plugins", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(
      new Response(JSON.stringify([
        {
          name: "plugin-a",
          status: "enabled",
          descriptor: {
            ui: {
              bundle_url: "http://a:3000/bundle.js",
              bundle_hash: "sha256-AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
            },
          },
        },
        {
          name: "plugin-b",
          status: "enabled",
          descriptor: {
            ui: {
              bundle_url: "http://b:3000/bundle.js",
              bundle_hash: "sha256-BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB=",
            },
          },
        },
      ]), { status: 200 }),
    );

    // Mock script loading and simulate plugin registration.
    const origCreateElement = document.createElement.bind(document);
    vi.spyOn(document, "createElement").mockImplementation((tag: string) => {
      const el = origCreateElement(tag);
      if (tag === "script") {
        setTimeout(() => {
          // Simulate what the IIFE bundle would do.
          const src = (el as HTMLScriptElement).src;
          if (src.includes("plugin-a")) {
            window.__RAT_REGISTER_PLUGIN!("plugin-a", {
              slots: { "sidebar-user": [(() => null) as any] },
              navItems: [{ label: "A", icon: "shield", href: "/a", priority: 1 }],
            });
          } else if (src.includes("plugin-b")) {
            window.__RAT_REGISTER_PLUGIN!("plugin-b", {
              slots: { "sidebar-user": [(() => null) as any] },
              navItems: [{ label: "B", icon: "puzzle", href: "/b", priority: 2 }],
            });
          }
          el.dispatchEvent(new Event("load"));
        }, 0);
      }
      return el;
    });

    const reg = await loadPlugins("test-nonce");

    expect(reg.slots["sidebar-user"]).toHaveLength(2);
    expect(reg.navItems).toHaveLength(2);
    expect(reg.navItems[0].label).toBe("A");
    expect(reg.navItems[1].label).toBe("B");
  });

  it("refuses to inject script for unsigned bundle in strict mode (default)", async () => {
    // Default mode: NEXT_PUBLIC_RAT_ALLOW_UNSIGNED_BUNDLES is not set.
    vi.stubEnv("NEXT_PUBLIC_RAT_ALLOW_UNSIGNED_BUNDLES", "");
    const errorSpy = vi.spyOn(console, "error").mockImplementation(() => {});

    vi.spyOn(globalThis, "fetch").mockResolvedValue(
      new Response(JSON.stringify([
        {
          name: "evil-plugin",
          status: "enabled",
          // bundle_url present, bundle_hash absent → must be rejected.
          descriptor: { ui: { bundle_url: "http://evil:3000/bundle.js" } },
        },
      ]), { status: 200 }),
    );

    const reg = await loadPlugins("test-nonce");

    const scripts = document.head.querySelectorAll("script[src*='bundle.js']");
    expect(scripts.length).toBe(0);
    expect(errorSpy).toHaveBeenCalledWith(
      expect.stringContaining("refusing to load unsigned bundle"),
    );
    // Promise still resolves cleanly — the wider load doesn't hang.
    expect(reg).toEqual({ slots: {}, navItems: [], routes: [] });

    vi.unstubAllEnvs();
  });

  it("loads unsigned bundle with a warning when NEXT_PUBLIC_RAT_ALLOW_UNSIGNED_BUNDLES=true", async () => {
    vi.stubEnv("NEXT_PUBLIC_RAT_ALLOW_UNSIGNED_BUNDLES", "true");
    const warnSpy = vi.spyOn(console, "warn").mockImplementation(() => {});
    const errorSpy = vi.spyOn(console, "error").mockImplementation(() => {});

    vi.spyOn(globalThis, "fetch").mockResolvedValue(
      new Response(JSON.stringify([
        {
          name: "dev-plugin",
          status: "enabled",
          descriptor: { ui: { bundle_url: "http://dev:3000/bundle.js" } },
        },
      ]), { status: 200 }),
    );

    const origCreateElement = document.createElement.bind(document);
    vi.spyOn(document, "createElement").mockImplementation((tag: string) => {
      const el = origCreateElement(tag);
      if (tag === "script") {
        setTimeout(() => el.dispatchEvent(new Event("load")), 0);
      }
      return el;
    });

    await loadPlugins("test-nonce");

    const scripts = document.head.querySelectorAll("script[src*='bundle.js']");
    expect(scripts.length).toBe(1);
    // No SRI applied — integrity is unset (jsdom returns undefined or "").
    expect((scripts[0] as HTMLScriptElement).integrity || "").toBe("");
    expect(warnSpy).toHaveBeenCalledWith(
      expect.stringContaining("script integrity not verified"),
    );
    expect(errorSpy).not.toHaveBeenCalled();

    vi.unstubAllEnvs();
  });

  it("merges routes from multiple plugins", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(
      new Response(JSON.stringify([
        {
          name: "plugin-a",
          status: "enabled",
          descriptor: {
            ui: {
              bundle_url: "http://a:3000/bundle.js",
              bundle_hash: "sha256-AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
            },
          },
        },
        {
          name: "plugin-b",
          status: "enabled",
          descriptor: {
            ui: {
              bundle_url: "http://b:3000/bundle.js",
              bundle_hash: "sha256-BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB=",
            },
          },
        },
      ]), { status: 200 }),
    );

    const origCreateElement = document.createElement.bind(document);
    vi.spyOn(document, "createElement").mockImplementation((tag: string) => {
      const el = origCreateElement(tag);
      if (tag === "script") {
        setTimeout(() => {
          const src = (el as HTMLScriptElement).src;
          if (src.includes("plugin-a")) {
            window.__RAT_REGISTER_PLUGIN!("plugin-a", {
              routes: [{ path: "/x/plugin-a", component: (() => null) as any }],
            });
          } else if (src.includes("plugin-b")) {
            window.__RAT_REGISTER_PLUGIN!("plugin-b", {
              routes: [
                { path: "/x/plugin-b", component: (() => null) as any },
                { path: "/x/plugin-b-admin", component: (() => null) as any },
              ],
            });
          }
          el.dispatchEvent(new Event("load"));
        }, 0);
      }
      return el;
    });

    const reg = await loadPlugins("test-nonce");

    expect(reg.routes).toHaveLength(3);
    expect(reg.routes[0].path).toBe("/x/plugin-a");
    expect(reg.routes[1].path).toBe("/x/plugin-b");
    expect(reg.routes[2].path).toBe("/x/plugin-b-admin");
  });
});
