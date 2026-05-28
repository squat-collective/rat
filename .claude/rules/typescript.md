---
paths:
  - "portal/**/*.ts"
  - "portal/**/*.tsx"
  - "sdk-typescript/**/*.ts"
---

# TypeScript rules (portal + SDK)

**Toolchain:** Node 20 · TS 5 · Next.js 14 (App Router) · shadcn/ui + Tailwind · SWR · CodeMirror 6 · Mermaid · Vitest · ESLint + Prettier.

## Style
- Strict types, **no `any`**. `"bronze" | "silver" | "gold"` union for layers.
- Server Components by default; `"use client"` only for interactivity.
- Fetch via **SWR hooks**, never `fetch` in `useEffect`. Error boundaries for graceful failure.
- No inline styles — Tailwind classes. No prop-drilling >2 levels.

## UI theme (underground / squat-collective)
Neon green + purple, no rounded corners (`--radius: 0px`); classes `rat-bg`, `brick-texture`, `brutal-card`, `neon-text`, `gradient-text`. `useScreenGlitch()` returns `{ triggerGlitch, GlitchOverlay, isGlitching }` — not callable directly. DataTable: zebra stripes, row numbers, type-colored values.

## Lint + test (gotchas)
- The portal has `.eslintrc.json` (`next/core-web-vitals`) — **required**, or `next lint` drops into an interactive prompt and dies in CI.
- CI runs `npm ci`, which **hard-fails on any package.json/package-lock.json version mismatch**. When you bump a version, the lock's `version` field must match — sync both.
- Anonymous components fail `react/display-name` — name them and set `.displayName`.
- Auth moved to an **adapter** (`@/lib/auth/client`). `ApiProvider` no longer gates rendering on session loading / auto-signs-out — don't reintroduce or test that removed behavior.
- `server-api` lists use `revalidate: 0` (== `no-store`) deliberately, so a freshly created/deleted pipeline is never served stale — not ISR.

## Before pushing
`make test-ts` (SDK vitest + portal vitest + `next lint`). `make portal-typecheck` builds the SDK first (it's a prereq).
