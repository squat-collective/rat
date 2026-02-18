# TypeScript Portal & SDK Code Review

**Reviewer**: Senior TypeScript / React Architect
**Date**: 2026-02-16
**Scope**: All TypeScript source files in `portal/` (40+ components, 4 hooks, 8 lib modules) and `sdk-typescript/` (transport, client, 12 resources, models)
**Branch**: `feat/ratd-health-and-pipelines`

---

## Summary Statistics

| Severity | Count |
|----------|-------|
| Critical | 7 |
| High | 15 |
| Medium | 22 |
| Low | 14 |
| **Total** | **58** |

---

## 1. Type Safety

### TS-1: SDK transport uses unsafe `as unknown as Record<string, unknown>` pattern — `HIGH`

**File**: `sdk-typescript/src/resources/*.ts` (~15 instances)

Every resource method that sends a JSON body double-casts through `unknown`:
```typescript
await this.transport.request("POST", path, {
  json: params as unknown as Record<string, unknown>,
});
```

**Fix**: Make the transport generic or accept `unknown` directly:
```typescript
async request<T>(method: string, path: string, options?: { json?: unknown }): Promise<T>
```

### TS-2: Transport `request()` return type is `Promise<any>` — `HIGH`

**File**: `sdk-typescript/src/transport.ts`, line 30

The core transport method returns `any`, defeating TypeScript's type system at the API boundary.

**Fix**: Return `Promise<unknown>` and let callers assert types.

### TS-3: No runtime type validation on API responses — `MEDIUM`

**File**: `sdk-typescript/src/transport.ts`, lines 55-58

API responses are cast directly to the expected type with `as T`. A schema mismatch silently produces an object with wrong types.

**Fix**: Add optional runtime validation using Zod schemas.

### TS-4: `NodeSSE` type lacks proper event typing — `MEDIUM`

**File**: `portal/src/hooks/use-sse.ts`, lines 15-22

The EventSource message types are parsed with `JSON.parse()` without validation.

### TS-5: Multiple `any` casts in portal components — `MEDIUM`

**Files**: Various portal components

Despite the project rule "no `any` types", several components use `any` for event handlers and dynamic data.

### TS-6: Error class does not extend properly for instanceof checks — `LOW`

**File**: `sdk-typescript/src/errors.ts`

---

## 2. React Patterns

### RP-1: Side effect in render body — `CRITICAL`

**File**: `portal/src/app/runs/[id]/page.tsx`, lines 107-109

```typescript
const { mutate } = useSWRConfig();
if (run?.status === "success" || run?.status === "failed") {
  mutate((key: string) => key.startsWith("pipelines"));
}
```

SWR mutation is called directly in the render body (not in `useEffect`). This can cause infinite re-renders: mutate triggers re-render → render sees same status → calls mutate again → infinite loop.

**Fix**: Move to `useEffect` with proper dependency:
```typescript
useEffect(() => {
  if (run?.status === "success" || run?.status === "failed") {
    mutate((key: string) => key.startsWith("pipelines"));
  }
}, [run?.status, mutate]);
```

### RP-2: DataTable does not memoize columns — `MEDIUM`

**File**: `portal/src/components/data-table.tsx`

Columns are recreated on every render, causing unnecessary re-renders.

### RP-3: CodeEditor creates new extensions array on every render — `MEDIUM`

**Files**: `portal/src/components/code-editor.tsx`, `portal/src/components/sql-editor.tsx`

The `extensions` array passed to CodeMirror is recreated each render, triggering full editor reconfiguration.

### RP-4: Pipeline detail page is a 1035-line monolith — `CRITICAL`

**File**: `portal/src/app/pipelines/[ns]/[layer]/[name]/page.tsx` (1035 lines)

This is the largest component in the portal. It handles pipeline metadata, code editing, preview, run history, quality tests, triggers, scheduling, and configuration in a single component.

**Fix**: Break into sub-components: `PipelineEditor`, `PipelinePreview`, `PipelineRunHistory`, `PipelineConfig`, `PipelineTriggers`.

### RP-5: Landing zone page is 938 lines — `HIGH`

**File**: `portal/src/app/landing/[ns]/[name]/page.tsx` (938 lines)

Same monolith pattern. Should be broken into file manager, metadata editor, trigger configuration.

### RP-6: `useState` for form fields should use `useReducer` — `LOW`

**File**: `portal/src/app/landing/[ns]/[name]/page.tsx`

8+ independent `useState` calls for related form fields.

### RP-7: Event handlers recreated on every render — `LOW`

Multiple components create new function references for callbacks without `useCallback`.

### RP-8: Prop drilling in landing zone page — `LOW`

Deeply nested function calls passing 5+ parameters.

---

## 3. Next.js Patterns

### NX-1: Home page uses `"use client"` unnecessarily — `MEDIUM`

**File**: `portal/src/app/page.tsx`

The home page (dashboard) is a client component but contains mostly static content and SWR data fetching that could work as a server component with client islands.

### NX-2: No route-level error boundaries — `CRITICAL`

**File**: No `error.tsx` files exist in any route directory

If any page throws during render, the entire application crashes with the default Next.js error page. No graceful degradation.

**Fix**: Add `error.tsx` to at least the top-level routes:
```
portal/src/app/error.tsx
portal/src/app/pipelines/error.tsx
portal/src/app/runs/error.tsx
portal/src/app/explorer/error.tsx
```

### NX-3: No loading states for route transitions — `MEDIUM`

No `loading.tsx` files exist. Route transitions show no feedback.

### NX-4: `generateMetadata` not used for SEO — `LOW`

No dynamic metadata generation for pipeline/run detail pages.

### NX-5: SSE hook creates non-serializable state — `LOW`

**File**: `portal/src/hooks/use-sse.ts`

Not an issue for CSR but prevents future SSR optimization.

---

## 4. State Management

### SM-1: Side effect mutate in render body — `CRITICAL`

(Same as RP-1 — the highest-severity finding in the portal)

### SM-2: SWR cache keys are raw strings, not namespaced — `HIGH`

**File**: `portal/src/hooks/use-api.ts`

Cache keys like `"runs"`, `"pipelines"`, `"namespaces"` are plain strings. Easy to collide or typo.

**Fix**: Use a key factory:
```typescript
const KEYS = {
  runs: () => "runs" as const,
  run: (id: string) => `runs/${id}` as const,
  pipelines: (ns?: string) => ns ? `pipelines/${ns}` : "pipelines",
} as const;
```

### SM-3: Retention page stores form state then re-derives it — `MEDIUM`

**File**: `portal/src/app/settings/retention/page.tsx`

### SM-4: File upload state not reset on navigation — `MEDIUM`

**File**: `portal/src/app/landing/[ns]/[name]/page.tsx`

---

## 5. Accessibility

### A11Y-1: Theme toggle has no accessible label — `MEDIUM`

**File**: `portal/src/components/theme-toggle.tsx`

### A11Y-2: File tree has no ARIA tree role — `MEDIUM`

**File**: `portal/src/components/file-tree.tsx`

### A11Y-3: DataTable has no caption or summary — `MEDIUM`

**File**: `portal/src/components/data-table.tsx`

### A11Y-4: Drag-and-drop zones not keyboard accessible — `HIGH`

**File**: `portal/src/app/landing/[ns]/[name]/page.tsx`

Drop zones use only mouse events (`onDragOver`, `onDrop`). No keyboard equivalent exists.

**Fix**: Add `<input type="file">` or add proper keyboard support.

### A11Y-5: Delete buttons lack descriptive labels — `MEDIUM`

Delete buttons only contain an icon (`<Trash2>`) with no `aria-label`.

**Fix**: Add `aria-label={`Delete file ${f.filename}`}`.

### A11Y-6: Form inputs use `<label>` without `htmlFor` — `MEDIUM`

Labels not programmatically associated with inputs.

---

## 6. Styling

### ST-1: Hardcoded color values instead of theme variables — `LOW`

**Files**: `portal/src/components/preview-panel.tsx`, `portal/src/app/runs/[id]/page.tsx`

Log level colors use raw Tailwind classes instead of semantic theme variables.

### ST-2: Inconsistent font-size micro-classes — `LOW`

Inconsistent use of `text-[9px]`, `text-[10px]`, `text-[11px]`, and `text-xs` for similar purposes.

---

## 7. Performance

### P-1: SSE log accumulation without limit — `HIGH`

**File**: `portal/src/hooks/use-sse.ts`, lines 42-48

Every log entry creates a new array via spread. For long-running pipelines producing thousands of log lines: O(n^2) total memory.

**Fix**: Use `useRef` for buffer, batch updates every 100ms. Cap at max displayed log lines.

### P-2: `useScreenGlitch()` returns new object reference every render — `MEDIUM`

**File**: `portal/src/components/screen-glitch.tsx`

**Fix**: Memoize return value with `useMemo`.

### P-3: Dynamic import for CodeEditor missing error boundary — `LOW`

**File**: `portal/src/app/pipelines/[ns]/[layer]/[name]/page.tsx`

If chunk fails to load, entire page crashes.

### P-4: No pagination on list pages — `HIGH`

**Files**: `portal/src/app/pipelines/pipelines-client.tsx`, `portal/src/app/runs/page.tsx`, `portal/src/app/explorer/page.tsx`

All list pages fetch and render the complete dataset. Slow for large installations.

**Fix**: Implement cursor-based or offset pagination.

### P-5: `processedGroups` memo triggers cascade of API calls — `LOW`

**File**: `portal/src/app/landing/[ns]/[name]/page.tsx`

---

## 8. Error Handling

### EH-1: Silent error swallowing in SSE handlers — `HIGH`

**File**: `portal/src/hooks/use-sse.ts`

```typescript
} catch {
  // ignore parse errors
}
```

JSON parse errors in SSE data silently swallowed. If API format changes, UI silently fails.

**Fix**: `console.warn("Failed to parse SSE log entry:", e)`

### EH-2: Generic `catch {}` blocks without logging — `MEDIUM`

**Files**: Multiple async handlers across landing zone page, retention settings, pipeline retention

```typescript
} catch {
  triggerGlitch();
}
```

Visual feedback provided but error details lost. Debugging impossible.

**Fix**: Log before triggering glitch: `console.error("Failed to save:", e)`

### EH-3: No timeout or retry on SSE connection — `MEDIUM`

**File**: `portal/src/hooks/use-sse.ts`

SSE connection failure closes permanently. No reconnection logic.

**Fix**: Implement reconnection with exponential backoff.

### EH-4: Server API fetch has no timeout — `LOW`

**File**: `portal/src/lib/server-api.ts`

No `AbortController` with timeout on server-side fetches.

---

## 9. Security

### SEC-1: Preview execution lacks UI warning — `MEDIUM`

**File**: `portal/src/hooks/use-preview.ts`

No visual indicator that preview actually executes code.

### SEC-2: Download auto-click with unsanitized filename — `LOW`

**File**: `portal/src/app/landing/[ns]/[name]/page.tsx`

Filename from S3 path splitting without sanitization.

**Fix**: `const safeFilename = filename.replace(/[^a-zA-Z0-9._-]/g, "_")`

---

## 10. Testing

### T-1: SDK client test is stale — asserts 7 resources, client has 12 — `CRITICAL`

**File**: `sdk-typescript/tests/client.test.ts`

Test name says "exactly 7 resource properties" but the client now has 12 resources (adding landing, triggers, quality, lineage, retention).

**Fix**: Update test to cover all 12 resources and verify total count.

### T-2: No tests for 5 SDK resource classes — `HIGH`

**File**: `sdk-typescript/tests/resources.test.ts`

Missing tests for `LandingResource`, `TriggersResource`, `QualityResource`, `LineageResource`, `RetentionResource`.

### T-3: Portal has only 2 test files — `CRITICAL`

**Files**: `portal/src/lib/__tests__/annotations.test.ts`, `portal/src/lib/__tests__/validation.test.ts`

The entire portal (40+ components, 4 hooks, 8 lib modules) has only 2 test files. Critical untested areas: all SWR hooks, SSE hook, preview hook, editor hook, pipeline utilities, server API, data table, create dialogs.

### T-4: Existing tests have no coverage for edge cases — `LOW`

No tests for malformed annotations, unicode, Windows line endings, mixed comment styles.

### T-5: No component tests using Testing Library — `MEDIUM`

`@testing-library/react` is in dependencies but no component tests exist.

---

## 11. SDK Design

### SDK-1: Missing barrel exports for `TriggersResource` and `RetentionResource` — `HIGH`

**File**: `sdk-typescript/src/resources/index.ts`

Barrel file exports 10 resources but omits 2.

### SDK-2: No cancellation support (AbortController) in SDK — `MEDIUM`

**File**: `sdk-typescript/src/transport.ts`

Long-running requests cannot be cancelled by consumers.

### SDK-3: `LandingResource.uploadFile()` likely needs multipart support — `MEDIUM`

**File**: `sdk-typescript/src/resources/landing.ts`

### SDK-4: Transport retry delay is linear, should be exponential — `LOW`

**File**: `sdk-typescript/src/transport.ts`

```typescript
await new Promise((r) => setTimeout(r, 500 * (attempt + 1)));
```

**Fix**: `Math.min(500 * 2 ** attempt, 10000)`

### SDK-5: No request/response interceptors — `LOW`

### SDK-6: `RatClientOptions` duplicates `ClientConfig` — `LOW`

---

## 12. Code Organization

### CO-1: Duplicated `RAT_LOGO` constant — `MEDIUM`

**Files**: `portal/src/app/page.tsx`, `portal/src/components/nav/sidebar.tsx`

Same multi-line ASCII art string duplicated.

### CO-2: Duplicated `formatBytes` function — `MEDIUM`

**Files**: `portal/src/lib/utils.ts`, `portal/src/components/preview-panel.tsx`

### CO-3: `server-api.ts` duplicates SDK types — `HIGH`

**File**: `portal/src/lib/server-api.ts`

Server-side API types duplicate the SDK model types. Will drift as SDK evolves.

**Fix**: Import types from the SDK.

### CO-4: `server-api.ts` uses wrong API endpoint for features — `CRITICAL`

**File**: `portal/src/lib/server-api.ts`, line 77

```typescript
features: () => apiFetch<FeaturesResponse>("/health/features"),
```

SDK uses `/api/v1/features`. Server API uses `/health/features`. One is wrong — settings page may fail.

**Fix**: Verify correct endpoint and unify.

---

## 13. Forms & Validation

### FV-1: No client-side validation on form submission — `HIGH`

**File**: `portal/src/components/create-pipeline-dialog.tsx`

The `validateName()` function exists in `lib/validation.ts` but is NOT wired into the create dialog.

### FV-2: Number inputs accept negative values — `MEDIUM`

**File**: `portal/src/app/settings/retention/page.tsx`

`Number(e.target.value)` can produce `NaN` or negative values.

### FV-3: Landing zone metadata form has no dirty tracking — `LOW`

---

## 14. Data Fetching

### DF-1: No error states shown for failed SWR fetches — `HIGH`

Most SWR hooks destructure `{ data, isLoading }` but ignore `error`. API errors result in silent empty state.

**Fix**: Destructure and display errors.

### DF-2: SWR `refreshInterval` not used for active runs — `MEDIUM`

Run list page does not auto-refresh. Active runs appear stale.

### DF-3: Server components re-fetch on every navigation — `LOW`

`cache: "no-store"` disables all caching. Every navigation triggers fresh server-side fetch.

**Fix**: Use `next.revalidate` for time-based caching.

---

## Priority Recommendations

### Immediate (This Sprint)

1. **RP-1 / SM-1**: Fix the render-body side effect in `runs/[id]/page.tsx` — can cause infinite re-renders
2. **CO-4**: Fix the `/health/features` vs `/api/v1/features` endpoint mismatch — this is a bug
3. **T-1**: Update the stale SDK client test — gives false confidence
4. **TS-1**: Fix SDK transport to accept `unknown` JSON bodies
5. **FV-1**: Wire up `validateName()` in create pipeline dialog

### Short Term (Next 2 Sprints)

6. **RP-4**: Break up the 1035-line pipeline detail page
7. **P-1**: Fix SSE log accumulation to prevent memory issues
8. **NX-2**: Add route-level error boundaries
9. **P-4**: Implement pagination for list pages
10. **T-3**: Add portal test coverage (start with hooks and utilities)

### Medium Term (Next Quarter)

11. **SDK-1**: Complete barrel exports
12. **CO-1, CO-2, CO-3**: Eliminate all code duplication
13. **A11Y-1 through A11Y-6**: Accessibility audit and fixes
14. **SDK-2**: Add AbortController support to SDK
15. **DF-1**: Show error states for all SWR fetches

---

*Review generated on 2026-02-16. All file paths are relative to the repository root.*
