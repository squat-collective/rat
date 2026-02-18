# RAT Portal

> Next.js 14 web IDE for RAT.
> Underground/squat-collective UI aesthetic — neon green, purple, sharp corners, glitch effects.

## Quick Start

```bash
# From project root
make dev-portal     # Hot reload on http://localhost:3000 (builds SDK first)
make portal-build   # Production standalone build
```

## Routes

| Route | Page | Description |
|-------|------|-------------|
| `/` | Home | Dashboard — stats, quick links, recent runs |
| `/pipelines` | Pipelines | List/search pipelines, create new, layer-colored badges |
| `/pipelines/[ns]/[layer]/[name]` | Pipeline Detail | Overview, files, recent runs, run button |
| `/editor` | Editor | S3-backed file editor — file tree, CodeMirror, multi-tab |
| `/query` | Query | SQL console — schema sidebar, CodeMirror, DataTable results |
| `/runs` | Runs | Run history — status table, auto-refresh 5s |
| `/runs/[id]` | Run Detail | Logs, cancel, info cards, auto-refresh 3s |
| `/explorer` | Explorer | Table browser — grouped by namespace/layer |
| `/explorer/[ns]/[layer]/[name]` | Table Detail | Schema + preview tabs |

## Architecture

```
src/
  app/              # Next.js App Router pages (9 routes)
  components/       # Shared components
    ui/             # shadcn/ui primitives (14 components)
    nav/            # Sidebar navigation
    data-table.tsx  # Zebra-striped table with type-colored cells
    sql-editor.tsx  # CodeMirror 6 with schema autocomplete
    schema-tree.tsx # Expandable namespace > layer > table tree
    file-tree.tsx   # S3 file browser (builds tree from flat FileInfo[])
    loading.tsx     # Loading spinner with neon pulse
    screen-glitch.tsx  # Error glitch effect overlay
    create-pipeline-dialog.tsx
  hooks/
    use-api.ts      # 13 SWR hooks wrapping SDK resources
    use-editor.ts   # File save mutation, tab state, language detection
  providers/
    api-provider.tsx  # RatClient context + SWR config
  lib/
    utils.ts        # cn() utility
    api-client.ts   # API URL config
    navigation.ts   # Static nav items
```

## Data Flow

```
Browser → SWR hook → RatClient (SDK) → fetch → ratd REST API
                                                         ↓
                                                    Go handlers
                                                    ↓        ↓
                                              Postgres    S3/MinIO
```

- **No auth** in Community Edition — no NextAuth, no tokens, no session
- **SWR** handles caching, revalidation, and error retry
- **ApiProvider** creates a single `RatClient` instance

## SWR Hooks

| Hook | Resource | Auto-refresh |
|------|----------|-------------|
| `usePipelines(params?)` | `pipelines.list()` | — |
| `usePipeline(ns, layer, name)` | `pipelines.get()` | — |
| `useRuns(params?)` | `runs.list()` | 5s |
| `useRun(id)` | `runs.get()` | 3s (stops on terminal) |
| `useRunLogs(id)` | `runs.logs()` | 3s |
| `useTables(params?)` | `tables.list()` | — |
| `useTable(ns, layer, name)` | `tables.get()` | — |
| `useTablePreview(ns, layer, name)` | `tables.preview()` | — |
| `useFileTree(prefix?)` | `storage.list()` | — |
| `useFileContent(path)` | `storage.read()` | — |
| `useQuerySchema()` | `tables.list()` → build tree | — |
| `useNamespaces()` | `namespaces.list()` | — |
| `useFeatures()` | `health.getFeatures()` | — |

## UI Theme

Underground/squat-collective inspired — consistent with v1:

- **Colors**: Neon green primary, purple accent, cyan highlights
- **Corners**: `--radius: 0px` — all sharp/square
- **Effects**: CRT scanlines, rat ASCII watermark, dripping pixels, screen glitch on errors
- **CSS classes**: `brutal-card`, `list-item`, `error-block`, `neon-text`, `gradient-text`, `rat-bg`, `brick-texture`
- **DataTable**: Zebra stripes, row numbers, type-colored values (cyan=number, red=null, green=boolean)
- **Font**: JetBrains Mono

## Tech Stack

| Dependency | Version | Purpose |
|------------|---------|---------|
| Next.js | 14 | App Router, standalone output |
| React | 18 | UI framework |
| TypeScript | 5 | Type safety |
| SWR | 2 | Data fetching + caching |
| CodeMirror | 6 | SQL/Python/YAML editors |
| shadcn/ui | — | UI primitives (Radix + Tailwind) |
| Tailwind CSS | 3 | Utility-first styling |
| lucide-react | — | Icons |

## Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `NEXT_PUBLIC_API_URL` | `http://localhost:8080` | ratd API URL (browser) |

## Docker

```bash
make portal-build    # Standalone Next.js build via Docker
make build           # Build all images (includes portal)
```

The portal Dockerfile is a 3-stage build:
1. **deps** — Build SDK, install portal dependencies
2. **builder** — Next.js production build
3. **runner** — `node:20-alpine`, standalone output, port 3000
