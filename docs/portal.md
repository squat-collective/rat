# Portal Architecture

> Next.js 14 web IDE for RAT v2.
> Source: `portal/src/`
> Build: `make portal-build` | Dev: `make dev-portal` (port 3000)

---

## Overview

The portal is the **only user interface** for RAT. It's a client-side Next.js app that talks to ratd via the TypeScript SDK. Community Edition has no auth — the portal renders immediately with full access.

```
Browser → SWR hook → RatClient (SDK) → fetch() → ratd (Go API :8080)
```

---

## Route Map

| Route | Component | Description |
|-------|-----------|-------------|
| `/` | `app/page.tsx` | Dashboard — pipeline/table/run counts, quick links, recent runs |
| `/pipelines` | `app/pipelines/page.tsx` | Pipeline list — search, layer-colored badges, create dialog |
| `/pipelines/[ns]/[layer]/[name]` | `app/pipelines/[ns]/[layer]/[name]/page.tsx` | Pipeline detail — files, recent runs, run trigger button |
| `/editor` | `app/editor/page.tsx` | S3-backed file editor — file tree sidebar, CodeMirror tabs, Ctrl-S save |
| `/query` | `app/query/page.tsx` | SQL console — schema tree sidebar, CodeMirror, DataTable results |
| `/runs` | `app/runs/page.tsx` | Run history — status table, 5s auto-refresh |
| `/runs/[id]` | `app/runs/[id]/page.tsx` | Run detail — logs, cancel button, duration/rows cards |
| `/explorer` | `app/explorer/page.tsx` | Table browser — grouped by namespace > layer, size/row badges |
| `/explorer/[ns]/[layer]/[name]` | `app/explorer/[ns]/[layer]/[name]/page.tsx` | Table detail — Schema tab + Preview tab |

---

## Component Inventory

### Layout & Navigation

| Component | Location | Purpose |
|-----------|----------|---------|
| `AppShell` | `components/app-shell.tsx` | Sidebar + content area, dripping pixel animations |
| `Sidebar` | `components/nav/sidebar.tsx` | ASCII rat logo, 6 nav items, theme toggle |
| `ThemeToggle` | `components/theme-toggle.tsx` | Dark/Light/System modes |

### Data Display

| Component | Location | Purpose |
|-----------|----------|---------|
| `DataTable` | `components/data-table.tsx` | Zebra-striped table, row numbers, type-colored cells |
| `SchemaTree` | `components/schema-tree.tsx` | Expandable namespace > layer > table tree for autocomplete |
| `FileTree` | `components/file-tree.tsx` | Builds tree from flat `FileInfo[]`, layer-colored folders |
| `Loading` | `components/loading.tsx` | Spinner with neon pulse + `PageSkeleton` |

### Editors

| Component | Location | Purpose |
|-----------|----------|---------|
| `SqlEditor` | `components/sql-editor.tsx` | CodeMirror 6 — SQL syntax, schema autocomplete, Ctrl-Enter execute |

### Feedback

| Component | Location | Purpose |
|-----------|----------|---------|
| `ScreenGlitch` | `components/screen-glitch.tsx` | `useScreenGlitch()` hook — 600ms glitch overlay on errors |

### Dialogs

| Component | Location | Purpose |
|-----------|----------|---------|
| `CreatePipelineDialog` | `components/create-pipeline-dialog.tsx` | Form: namespace, name, layer, type, source, unique_key |
| `PipelineMergeStrategy` | `components/pipeline-merge-strategy.tsx` | Merge strategy settings card — strategy selector, conditional fields, save to config.yaml |

### shadcn/ui Primitives (14)

`button`, `badge`, `card`, `input`, `textarea`, `select`, `dialog`, `label`, `skeleton`, `tabs`, `separator`, `tooltip`, `dropdown-menu`, `scroll-area`

---

## Hooks

### SWR Data Hooks (`hooks/use-api.ts`)

| Hook | SDK Method | Refresh |
|------|-----------|---------|
| `usePipelines(params?)` | `pipelines.list()` | — |
| `usePipeline(ns, layer, name)` | `pipelines.get()` | — |
| `useRuns(params?)` | `runs.list()` | 5s |
| `useRun(id)` | `runs.get()` | 3s (stops on terminal status) |
| `useRunLogs(id)` | `runs.logs()` | 3s |
| `useTables(params?)` | `tables.list()` | — |
| `useTable(ns, layer, name)` | `tables.get()` | — |
| `useTablePreview(ns, layer, name)` | `tables.preview()` | — |
| `useFileTree(prefix?)` | `storage.list()` | — |
| `useFileContent(path)` | `storage.read()` | — |
| `useQuerySchema()` | `tables.list()` → tree | — |
| `useNamespaces()` | `namespaces.list()` | — |
| `useFeatures()` | `health.getFeatures()` | — |
| `usePipelineConfig(ns, layer, name)` | `storage.read()` → YAML parse | — |

### Editor Hooks (`hooks/use-editor.ts`)

| Export | Type | Purpose |
|--------|------|---------|
| `useSaveFile()` | Hook → `{ save, saving }` | Mutation: `storage.write(path, content)` |
| `detectLanguage(path)` | Function | Maps file extension → CodeMirror language |
| `OpenTab` | Type | `{ path, content, originalContent, language }` |

---

## Providers

### `ApiProvider` (`providers/api-provider.tsx`)

Creates a single `RatClient` instance using `NEXT_PUBLIC_API_URL` and provides it via React context. SWR global config with console error handler.

```tsx
<SWRConfig value={{ onError: (err) => console.error("[SWR]", err) }}>
  <ApiContext.Provider value={client}>
    {children}
  </ApiContext.Provider>
</SWRConfig>
```

Access the client in any component:
```tsx
import { useApiClient } from "@/providers/api-provider";
const api = useApiClient();
```

---

## Provider Stack

```tsx
<ThemeProvider defaultTheme="dark">
  <ApiProvider>
    <AppShell>
      {children}   {/* page content */}
    </AppShell>
  </ApiProvider>
</ThemeProvider>
```

No auth providers, no session providers — Community Edition is anonymous.

---

## UI Theme

Underground/squat-collective aesthetic, consistent with v1:

| Aspect | Value |
|--------|-------|
| Primary color | Neon green `hsl(142 72% 45%)` |
| Accent | Purple `hsl(280 100% 65%)` |
| Cyan highlight | `hsl(180 100% 50%)` |
| Border radius | `0px` — all sharp corners |
| Font | JetBrains Mono |
| Background | Noise overlay + CRT scanlines + ASCII rat watermark |

### CSS Classes

| Class | Effect |
|-------|--------|
| `brutal-card` | 4px offset shadow, sharp borders, hover glow |
| `list-item` | Left accent border, hover highlight |
| `error-block` | Red border, destructive background |
| `neon-text` | Green text shadow glow |
| `gradient-text` | Animated green→purple gradient |
| `rat-bg` | Faded ASCII rat watermark SVG |
| `brick-texture` | Subtle brick pattern background |
| `screen-glitch` | Full-page distortion effect (triggered by `useScreenGlitch`) |

### DataTable Conventions

- Zebra stripes: alternating `transparent` / `bg-muted/30`
- Row numbers: `01`, `02`, etc. in muted foreground
- Type coloring: cyan = number, red = null, green = boolean
- Sticky header with backdrop blur
- Footer: row count

### Layer Colors

| Layer | Color | Usage |
|-------|-------|-------|
| Bronze | Orange `#d97706` | Badges, borders, tree icons |
| Silver | Slate `#94a3b8` | Badges, borders, tree icons |
| Gold | Yellow `#eab308` | Badges, borders, tree icons |

---

## Key Patterns

### v1 → v2 Differences (Portal)

| Aspect | v1 | v2 |
|--------|----|----|
| Auth | NextAuth.js v5 + Keycloak | None (Community) |
| Routing | `workspace` param | `namespace` param |
| Query rows | `unknown[][]` (arrays) | `Record<string, unknown>[]` (objects) |
| File tree | API returns tree structure | Flat `FileInfo[]`, build client-side |
| API errors | JSON `{ detail }` | Plain text body |
| Port | 8090 | 3000 |
| Provider stack | Theme → Session → API → Workspace | Theme → API only |

### Suspense Boundaries

Pages using `useSearchParams()` (e.g., `/editor`) must be wrapped in `<Suspense>` for Next.js 14 App Router static generation.

### Auto-refresh Pattern

Runs use SWR `refreshInterval` for live updates. The interval stops when the run reaches a terminal status (`success`, `failed`, `cancelled`).

---

## Build & Dev

```bash
make sdk-build        # Must build SDK first (portal depends on it)
make portal-build     # Full production build (standalone .next output)
make portal-typecheck # Type-check without building
make dev-portal       # Hot reload on port 3000 (builds SDK first)
```

Docker: 3-stage build → `node:20-alpine` runtime, port 3000.
