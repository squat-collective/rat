import type { PluginRegistry, PluginNavItem, PluginRoute, SlotComponent } from "./plugin-context";
import { PUBLIC_API_URL } from "@/lib/api-client";

/** Shape a plugin bundle passes to window.__RAT_REGISTER_PLUGIN. */
export type PluginRegistration = {
  slots?: Record<string, SlotComponent<any>[]>;
  navItems?: PluginNavItem[];
  routes?: PluginRoute[];
};

/** Extends Window with the RAT plugin registration hook. */
declare global {
  interface Window {
    __RAT_REGISTER_PLUGIN?: (name: string, reg: PluginRegistration) => void;
  }
}

/** Minimal shape of the plugin list API response. */
interface PluginListEntry {
  name: string;
  status: string;
  descriptor?: {
    ui?: {
      bundle_url?: string;
      // SHA-256 of the bundle in SRI format ("sha256-<base64>"). When present,
      // the loader sets <script integrity=…> so the browser refuses to execute
      // a tampered bundle. Plugins on older proto versions omit this.
      bundle_hash?: string;
      nav_items?: PluginNavItem[];
    };
  };
}

/**
 * Loads all enabled plugins that declare a UI bundle.
 *
 * 1. Fetches GET /api/v1/plugins?status=enabled
 * 2. Sets up window.__RAT_REGISTER_PLUGIN
 * 3. Injects <script> tags for each plugin bundle (via ratd proxy)
 * 4. Merges all registrations into a single PluginRegistry
 *
 * SRI strict mode (default): bundles without `descriptor.ui.bundle_hash` are
 * rejected at load time — no <script> tag is injected. Set the build-time env
 * `NEXT_PUBLIC_RAT_ALLOW_UNSIGNED_BUNDLES=true` to downgrade to a warning
 * (dev-only escape hatch; never enable in production).
 */
function allowUnsignedBundles(): boolean {
  return process.env.NEXT_PUBLIC_RAT_ALLOW_UNSIGNED_BUNDLES === "true";
}

export async function loadPlugins(nonce: string): Promise<PluginRegistry> {
  const registrations = new Map<string, PluginRegistration>();

  // Set up the global registration function before injecting scripts.
  window.__RAT_REGISTER_PLUGIN = (name: string, reg: PluginRegistration) => {
    registrations.set(name, reg);
  };

  try {
    const res = await fetch(`${PUBLIC_API_URL}/api/v1/plugins?status=enabled`);
    if (!res.ok) {
      console.warn("[plugin-loader] Failed to fetch plugins:", res.status);
      return emptyRegistry();
    }

    const plugins: PluginListEntry[] = await res.json();
    const withUI = plugins.filter(
      (p) => p.descriptor?.ui?.bundle_url,
    );

    if (withUI.length === 0) {
      return emptyRegistry();
    }

    // Load all bundles in parallel via <script> tags.
    await Promise.all(
      withUI.map(
        (p) =>
          new Promise<void>((resolve) => {
            const hash = p.descriptor?.ui?.bundle_hash;
            if (!hash && !allowUnsignedBundles()) {
              // Strict SRI: refuse to inject the <script> tag at all.
              // Resolve so the wider Promise.all doesn't hang on this plugin.
              console.error(
                `[plugin-loader] ${p.name} has no bundle_hash — refusing to load unsigned bundle. ` +
                  `Set NEXT_PUBLIC_RAT_ALLOW_UNSIGNED_BUNDLES=true for dev.`,
              );
              resolve();
              return;
            }
            const script = document.createElement("script");
            script.src = `${PUBLIC_API_URL}/api/v1/plugins/${p.name}/ui/bundle.js`;
            script.nonce = nonce;
            script.async = true;
            if (hash) {
              // SRI: the browser hashes the fetched bytes and refuses to
              // execute the script if they don't match. crossOrigin is
              // required because the bundle is fetched cross-origin (portal
              // origin → ratd origin) and SRI demands a CORS-clean response.
              script.integrity = hash;
              script.crossOrigin = "anonymous";
            } else {
              // ALLOW_UNSIGNED_BUNDLES escape hatch — preserve prior behaviour.
              console.warn(
                `[plugin-loader] ${p.name} has no bundle_hash — script integrity not verified. ` +
                  `Add bundle_hash to its Describe() response to enable SRI.`,
              );
            }
            script.onload = () => resolve();
            script.onerror = () => {
              console.warn(`[plugin-loader] Failed to load bundle for plugin: ${p.name}`);
              resolve(); // Don't block other plugins on failure.
            };
            document.head.appendChild(script);
          }),
      ),
    );

    return mergeRegistrations(registrations);
  } catch (err) {
    console.error("[plugin-loader] Error loading plugins:", err);
    return emptyRegistry();
  }
}

/** Merges all plugin registrations into a single PluginRegistry. */
function mergeRegistrations(
  registrations: Map<string, PluginRegistration>,
): PluginRegistry {
  const slots: Record<string, SlotComponent<any>[]> = {};
  const navItems: PluginNavItem[] = [];
  const routes: PluginRoute[] = [];

  for (const [, reg] of registrations) {
    // Merge slots.
    if (reg.slots) {
      for (const [slotName, components] of Object.entries(reg.slots)) {
        if (!slots[slotName]) {
          slots[slotName] = [];
        }
        slots[slotName].push(...components);
      }
    }

    // Merge nav items.
    if (reg.navItems) {
      navItems.push(...reg.navItems);
    }

    // Merge routes.
    if (reg.routes) {
      routes.push(...reg.routes);
    }
  }

  return { slots, navItems, routes };
}

function emptyRegistry(): PluginRegistry {
  return { slots: {}, navItems: [], routes: [] };
}
