# API Deprecation Strategy

> **Status**: Active
> **Last updated**: 2026-02-16

## Overview

RAT follows a structured deprecation process for API changes that ensures backward compatibility while allowing the platform to evolve. This document defines the rules for deprecating, migrating, and removing API endpoints, fields, and proto messages.

---

## Deprecation Lifecycle

Every deprecated API element follows this lifecycle:

```
Active  -->  Deprecated  -->  Sunset  -->  Removed
```

| Phase | Duration | Behavior |
|-------|----------|----------|
| **Active** | Indefinite | Fully supported, documented, tested |
| **Deprecated** | Minimum 2 minor versions (e.g., v2.3 -> v2.5) | Still functional, but marked deprecated in docs and responses |
| **Sunset** | 1 minor version after deprecated period ends | Returns warning headers, logged server-side |
| **Removed** | After sunset | Returns 410 Gone with migration instructions |

---

## Rules

### REST API Endpoints

1. **New endpoints** must be added alongside old ones during the deprecation period.
2. **Deprecated endpoints** must include the `Deprecation` and `Sunset` HTTP headers in responses:
   ```
   Deprecation: true
   Sunset: Sat, 01 Nov 2026 00:00:00 GMT
   Link: </api/v1/pipelines/{ns}/{layer}/{name}/metadata>; rel="successor-version"
   ```
3. **Removed endpoints** return `410 Gone` with a JSON body pointing to the replacement:
   ```json
   {"error": {"code": "GONE", "message": "This endpoint has been removed. Use /api/v1/pipelines/{ns}/{layer}/{name}/metadata instead."}}
   ```

### REST API Response Fields

1. **New fields** can be added at any time (additive change, non-breaking).
2. **Renaming fields**: add the new name alongside the old one. The old field is deprecated.
3. **Removing fields**: follow the deprecation lifecycle. Mark with `// Deprecated:` in code.
4. **Type changes**: never change a field's type. Add a new field with the new type instead.

### Proto / gRPC

1. **Never** remove or renumber existing proto fields. Use `reserved` declarations.
2. **Never** change a field's type. Add a new field with the next available number.
3. **Deprecated fields** must be annotated with `[deprecated = true]` in the proto definition.
4. **New messages/enums** can be added freely (non-breaking).
5. **Removed messages** must have their field numbers and names reserved.
6. See `proto/common/v1/common.proto` for the full field reservation policy.

### SDK (TypeScript)

1. **Deprecated methods** must be annotated with `@deprecated` JSDoc tag.
2. **New methods** can be added at any time.
3. **Removed methods** must throw an error with migration instructions for one version before full removal.

---

## Communication

When deprecating any API element:

1. **Add to CHANGELOG.md** under a "Deprecated" section.
2. **Update docs/api-spec.md** with deprecation notice and migration instructions.
3. **Log a warning** on the server when deprecated endpoints/fields are used.
4. **Notify users** via release notes and (for Pro) in-app deprecation warnings.

---

## Currently Deprecated

| Element | Deprecated In | Sunset | Replacement |
|---------|---------------|--------|-------------|
| `GET /api/v1/metadata/{ns}/pipeline/{layer}/{name}` | v2.1 | v2.3 | `GET /api/v1/pipelines/{ns}/{layer}/{name}/metadata` |
| `GET /api/v1/metadata/{ns}/quality/{layer}/{name}` | v2.1 | v2.3 | `GET /api/v1/pipelines/{ns}/{layer}/{name}/metadata/quality` |

---

## Examples

### Deprecating an endpoint

```go
// HandleOldEndpoint is deprecated — use HandleNewEndpoint instead.
// Deprecated in v2.1, sunset in v2.3.
func (s *Server) HandleOldEndpoint(w http.ResponseWriter, r *http.Request) {
    w.Header().Set("Deprecation", "true")
    w.Header().Set("Sunset", "Sat, 01 Nov 2026 00:00:00 GMT")
    w.Header().Set("Link", `</api/v1/new-endpoint>; rel="successor-version"`)
    // ... existing handler logic ...
}
```

### Deprecating a proto field

```protobuf
message Example {
  string old_field = 1 [deprecated = true]; // Deprecated: use new_field instead
  string new_field = 5;                     // Replacement for old_field
}
```
