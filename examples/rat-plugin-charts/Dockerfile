# Example RAT charts plugin — platform + portal plugin.
#
# This is the first example plugin with a real front-end build: the portal UI
# bundle is produced by esbuild bundling Recharts (stage `ui`), then embedded
# into the Go binary (stage `builder`).
#
# The platform module (a local `replace` dependency) is supplied as a NAMED
# build context. Build from the repo root:
#
#   docker build --build-context platform=platform \
#     -f examples/rat-plugin-charts/Dockerfile \
#     -t rat-plugin-charts examples/rat-plugin-charts
#
# Or just run `make build` from this directory.

# ── UI build: bundle the portal plugin (Recharts) with esbuild ──────
FROM node:20-alpine AS ui
WORKDIR /ui
COPY ui/package.json ./
RUN npm install --no-audit --no-fund
COPY ui/ ./
RUN npm run build

# ── Go build ────────────────────────────────────────────────────────
FROM golang:1.24-alpine AS builder
WORKDIR /build

# The platform module supplies the generated plugin/v1 ConnectRPC stubs.
COPY --from=platform . /platform/
COPY . .
# Embed the freshly built UI bundle (overrides any committed copy).
COPY --from=ui /ui/dist/bundle.js ./bundle.js

RUN go mod tidy && CGO_ENABLED=0 go build -ldflags="-s -w" -o /charts .

# ── Runtime ─────────────────────────────────────────────────────────
FROM alpine:3.21
RUN addgroup -S rat && adduser -S -G rat rat
COPY --from=builder /charts /charts
USER rat
EXPOSE 50092
ENTRYPOINT ["/charts"]
