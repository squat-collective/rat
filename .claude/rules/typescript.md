---
paths:
  - "portal/**/*.ts"
  - "portal/**/*.tsx"
  - "sdk-typescript/**/*.ts"
---

# TypeScript rules (portal + SDK)

**Toolchain:** Node 26 · TS 6 · React 19 · Next.js 16 (App Router, Turbopack) · shadcn/ui + Tailwind 4 · SWR · CodeMirror 6 · Mermaid · Vitest · ESLint 9 (flat config) + Prettier.

## Style
- Strict types, **no `any`**. `"bronze" | "silver" | "gold"` union for layers.
- Server Components by default; `"use client"` only for interactivity.
- Fetch via **SWR hooks**, never `fetch` in `useEffect`. Error boundaries for graceful failure.
- No inline styles — Tailwind classes. No prop-drilling >2 levels.

## UI theme (underground / squat-collective)
Neon green + purple, no rounded corners (`--radius: 0px`); classes `rat-bg`, `brick-texture`, `brutal-card`, `neon-text`, `gradient-text`. `useScreenGlitch()` returns `{ triggerGlitch, GlitchOverlay, isGlitching }` — not callable directly. DataTable: zebra stripes, row numbers, type-colored values.

## Lint + test (gotchas)
- `next lint` was **removed in Next 16**: the portal uses an ESLint 9 **flat config** (`eslint.config.mjs` spreading `eslint-config-next/core-web-vitals`), there is no `.eslintrc.json`, and the lint script is `eslint .`.
- **Tailwind v4 is CSS-first:** PostCSS uses `@tailwindcss/postcss`, `globals.css` does `@import "tailwindcss"` (not the three `@tailwind` directives), and the JS theme is pulled in via `@config "../../tailwind.config.ts"`. `autoprefixer` is gone (Tailwind/Lightning CSS handles prefixing).
- **eslint-plugin-react-hooks v6** (via eslint-config-next 16) ships React-Compiler rules. The portal doesn't run the compiler, so those are set to `warn` (non-blocking); the long-standing correctness rules stay errors.
- The request-middleware convention moved: `src/middleware.ts` → **`src/proxy.ts`** (export `proxy`) for Next 16.
- CI runs `npm ci`, which **hard-fails on any package.json/package-lock.json version mismatch**. When you bump a version, the lock's `version` field must match — sync both.
- Anonymous components fail `react/display-name` — name them and set `.displayName`.
- Auth moved to an **adapter** (`@/lib/auth/client`). `ApiProvider` no longer gates rendering on session loading / auto-signs-out — don't reintroduce or test that removed behavior.
- `server-api` lists use `revalidate: 0` (== `no-store`) deliberately, so a freshly created/deleted pipeline is never served stale — not ISR.

## Before pushing
`make test-ts` (SDK vitest + portal vitest + `eslint .`). `make portal-typecheck` builds the SDK first (it's a prereq).
