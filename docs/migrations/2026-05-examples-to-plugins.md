# `examples/` → `plugins/`

**Affects:** Operators with custom scripts, dotfiles, or local docs that
reference `examples/rat-plugin-*` paths. Does not affect a fresh
install or running plugins themselves.

## What changed

The `examples/` directory was renamed to `plugins/`. The 20 RAT plugins
that lived under `examples/` are now under `plugins/` instead. Each
plugin's internal structure is unchanged.

This reflects that the contents are first-class, shipped, supported
artifacts — not example code. The same directory now also feeds the
GHCR publishing pipeline (`.github/workflows/publish-plugins.yml`),
which builds each plugin and pushes to
`ghcr.io/squat-collective/rat-plugin-<name>`.

## Upgrade steps

1. **Anything that does `cd examples/rat-plugin-<name> && make build`**
   becomes `cd plugins/rat-plugin-<name> && make build`. The Makefile
   internals all reference the new path already.

2. **Custom docker-compose overlays** that referenced
   `./examples/rat-plugin-<name>/Dockerfile` should switch to
   `./plugins/rat-plugin-<name>/Dockerfile` — or, preferably, drop the
   local build and pull the published image from
   `ghcr.io/squat-collective/rat-plugin-<name>:latest` instead. See
   [`infra/docker-compose.plugins.yml`](../../infra/docker-compose.plugins.yml)
   for the canonical compose overlay.

3. **CI / scripts** with hardcoded `examples/rat-plugin` paths —
   `git grep examples/rat-plugin` in your own dotfiles will catch them.

4. **Bookmarked GitHub URLs** to `tree/main/examples/rat-plugin-*` will
   redirect; nothing to fix, but new bookmarks should use `plugins/`.

## Reverting

The rename was a single commit (`e879568`). If you've built tooling
around the old path and need to delay the migration, pin to
`feat/plugin-system-v3~1` until you've updated.

## Source

- Rename commit: `e879568` ("refactor(repo): move examples/ → plugins/")
- Reference-fix follow-up commit: `110b5cd`
