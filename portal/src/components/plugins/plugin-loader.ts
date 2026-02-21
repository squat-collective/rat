import type { PluginRegistry, PluginNavItem, SlotComponent } from "./plugin-context";
import { PUBLIC_API_URL } from "@/lib/api-client";

/** Shape a plugin bundle passes to window.__RAT_REGISTER_PLUGIN. */
export type PluginRegistration = {
  slots?: Record<string, SlotComponent<any>[]>;
  navItems?: PluginNavItem[];
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
 */
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
            const script = document.createElement("script");
            script.src = `${PUBLIC_API_URL}/api/v1/plugins/${p.name}/ui/bundle.js`;
            script.nonce = nonce;
            script.async = true;
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
  }

  return { slots, navItems };
}

function emptyRegistry(): PluginRegistry {
  return { slots: {}, navItems: [] };
}
