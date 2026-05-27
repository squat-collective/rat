# Migration Guides

Breaking changes between releases, with concrete upgrade steps. New
deployments don't need any of these — they're for upgrading existing
installations.

Each guide lives in its own file. Index sorted newest first:

| Date | Change | Affects |
|---|---|---|
| 2026-05 | [Plugin examples renamed to plugins](2026-05-examples-to-plugins.md) | Operators with custom scripts referencing `examples/rat-plugin-*` paths |
| 2026-05 | [Read paths now enforcement-filtered](2026-05-enforcement-filtered-reads.md) | **Pro deployments only** — list endpoints behave differently |
| 2026-05 | [Plugin restart re-Describes for fresh tokens](2026-05-plugin-restart-tokens.md) | Operators relying on the previous 30-second stale-token blind window |
| 2026-04 | [Internal listener moved to a separate port](2026-04-internal-listener-split.md) | Operators with custom docker-compose / k8s manifests that don't yet bind port 8090 |
| 2026-04 | [`RAT_LISTEN_ADDR` replaces `PORT` (legacy supported)](2026-04-listen-addr.md) | Operators using `PORT=8080` env var (still works as fallback) |

Older changes pre-Wave-1 weren't tracked here; see the wave commit
history (`git log --oneline | head -200`) for the full record.
