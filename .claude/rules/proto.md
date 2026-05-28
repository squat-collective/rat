---
paths:
  - "proto/**/*.proto"
  - "proto/buf.*.yaml"
---

# Proto rules

**Tooling:** buf.build (`bufbuild/buf:1.35.0`) for lint/breaking/codegen · ConnectRPC (Go + Python + TS).

## Conventions
- Versioned packages (`v1`, `v2`) — never break an existing proto.
- One `Request`/`Response` message per RPC — no shared request messages.
- Verb-noun service/RPC names; `snake_case` fields; comment every field.
- Shared types live in `common/v1/common.proto`.

## Hard rules
- `make proto` regenerates Go + Python + TS. Run `buf lint` before committing; `buf breaking` runs in CI.
- **Wire-protocol package names are frozen:** the proto packages stay `ratatouille.runner.v1` / `ratatouille.query.v1` etc. even though the product rebranded to RAT. Renaming them breaks the wire protocol with deployed plugins. Do NOT rename.
- **Python codegen plugin is pinned to `v33`** in `buf.gen.yaml` (`buf.build/protocolbuffers/python:v33.0`) so the generated `*_pb2.py` target the protobuf 6.33.x runtime that's installable from PyPI. Unpinned defaults to v35+ → gencode demands a 7.x runtime that doesn't exist → import-time `VersionError`. Keep this pin in lockstep with `runner`/`query` protobuf deps.
- The 5 "typed" protos (`executor/`, `cloud/`, `sharing/`, `permission/`, `identity/`) are **actively used** by the Pro plugin contract — do not delete them. Only `auth/v1` and `enforcement/v1` were ever removed.
