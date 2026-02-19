# ADR-016: Portal Plugin Slot System

## Status: Accepted

## Context

The portal needs a way for external packages to contribute React components that render at predefined locations in the UI. Use cases include adding user menus, navigation items, header widgets, or admin panels — without modifying the portal source code.

### Constraints

- The portal must build and run without any plugin packages installed
- The slot system must be generic — no knowledge of specific plugin implementations
- Plugin packages must be independently developed, built, and tested
- Dead-code elimination: when no plugin package is configured, bundles contain zero plugin code

## Decision

### Plugin Slot System

The portal provides three building blocks:

- **`PluginRegistryProvider`** — React context that holds a map of slot names to component arrays
- **`PluginSlot`** — renders all components registered for a named slot location
- **`PluginWrapper`** — conditionally loads an external plugin package via `NEXT_PUBLIC_PLUGIN_PACKAGE` env var

### Build-time gating

`NEXT_PUBLIC_PLUGIN_PACKAGE` names an npm package that exports `loadRegistry(): Promise<PluginRegistry>`. When unset, `PluginWrapper` is a passthrough — no dynamic import is attempted, no plugin code enters the bundle.

The dynamic import uses `/* webpackIgnore: true */` so webpack does not try to resolve the package at build time.

### Slot locations

Named extension points in the portal layout:

| Slot | Location |
|------|----------|
| `main-header` | Between LicenseBanner and content area |
| `sidebar-user` | Before theme toggle in sidebar |
| `sidebar-nav-extra` | After main navigation in sidebar |

### Plugin package contract

Any package can participate by exporting:

```typescript
export function loadRegistry(): Promise<PluginRegistry>;
```

Where `PluginRegistry` is `Record<string, React.ComponentType<any>[]>` — a map of slot names to component arrays.

### Slot arrays

Multiple components can register for the same slot (rendered in order). This allows independent packages to contribute to the same location without conflicts.

## Consequences

### Positive

- Portal has zero overhead when no plugin package is configured
- Fully generic — no knowledge of specific implementations baked into community code
- Extensible — new slots are a one-line `<PluginSlot>` addition
- Per-plugin feature gating is the plugin package's responsibility, not the portal's

### Negative

- Dynamic import adds a render cycle when a plugin package is configured (registry loads after mount)
- Plugin components must use the portal's existing design system (Tailwind classes) — no custom CSS injection

### Neutral

- `vitest.config.ts` added to portal for JSX transform + alias resolution
- `jsdom` added to devDependencies for React component testing
