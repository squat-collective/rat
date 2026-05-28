# Read paths now enforcement-filtered (Pro only)

**Affects:** Pro deployments running the enforcement plugin. **No
behaviour change in Community Edition** — the no-op authorizer
short-circuits the filter for unauthenticated requests.

## What changed

Pre-Wave-8, only `write` and `delete` endpoints went through the
enforcement plugin. Read endpoints (`GET /api/v1/pipelines`,
`GET /api/v1/pipelines/{ns}/{layer}/{name}`, `GET /api/v1/runs`,
`GET /api/v1/runs/{id}`, `GET /api/v1/namespaces`) returned every row
regardless of the caller's grants — a data leak.

Wave 8 added an `Authorizer.Filter()` method and wired it into the
read handlers. In a Pro deployment with the enforcement plugin
enabled, the list endpoints now post-filter to the subset the caller
can `read`.

## Semantic differences to be aware of

- **`total` in list responses now reflects the visible count**, not
  the raw SQL count. This is correct for "show the user what they
  have access to," but a UI that previously assumed `total` was the
  whole table's count needs to drop that assumption.

- **Pagination shifts from "page N of M" to "fetch more."** Because
  post-filter trims a page after fetching, page-2 with a 20-row limit
  may visibly drop below 20 even when the filtered total has more
  rows. For UIs at scale this is a follow-up to push filtering into
  SQL; until then, infinite-scroll semantics are recommended over
  numbered pagination.

- **`GET /pipelines/{ns}/{layer}/{name}` now returns 403** instead of
  the resource when the caller can't access it. Previously it
  returned 200 with the resource. The 404 case (pipeline doesn't
  exist) is unchanged.

- **`GET /runs/{id}` returns 403** for runs whose parent pipeline the
  caller can't read.

## Upgrade steps

1. **Re-test any UI / SDK code that processes `total` from list
   responses.** If you use `total` to drive numbered pagination or
   progress bars, plan for it to reflect *visible* count.

2. **Re-test 403 handling** on per-resource GETs — read paths now
   raise this where they used to return 200.

3. **No env-var or wiring changes required.** The fix activates
   automatically once the enforcement plugin is loaded — same as it
   did for write paths previously.

## Community deployments

Nothing changes. `NoopAuthorizer.Filter` returns the input slice
unmodified, and the read handlers detect a no-user context (community
single-user mode) before invoking the filter at all.

## Source

- Filter on Authorizer + wiring: commit `69a2f8d` ("feat(ratd):
  enforcement-filtered read paths for pipelines, runs, namespaces")
- Plugin-side `Filter` impl in `PluginAuthorizer`: same commit
- Audit context: `docs/audit-2026-05.md` and the fifth review summary
