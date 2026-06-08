# RAT lineage plugin — three-stage build:
#   1. node:20 builds the UI bundle (esbuild → dist/bundle.js)
#   2. golang:1.24 builds the Go binary (embeds the UI via go:embed)
#   3. alpine:3.21 runtime
#
# Build from the repo root so the platform module is available as a
# named context:
#
#   docker build \
#     --build-context platform=platform \
#     --build-context sdk=sdk-go \
#     -f examples/rat-plugin-lineage/Dockerfile \
#     -t rat-plugin-lineage examples/rat-plugin-lineage
#
# Or `make build` from this directory.
FROM node:26-alpine@sha256:144769ec3f32e8ee36b3cfde91e82bee25d9367b20f31a151f3f7eea3a2a8541 AS ui-builder
WORKDIR /ui
COPY ui/package.json ui/package-lock.json* ./
RUN npm install --no-audit --no-fund --silent || npm install --no-audit --no-fund
COPY ui/ ./
RUN npm run build

FROM golang:1.26-alpine@sha256:f23e8b227fb4493eabe03bede4d5a32d04092da71962f1fb79b5f7d1e6c2a17f AS go-builder
WORKDIR /build
COPY --from=platform . /platform/
COPY --from=sdk . /sdk-go/
COPY . .
# Pull the built UI bundle into the Go build context so go:embed picks
# it up. ui/dist/bundle.js is the only file we need at this stage.
COPY --from=ui-builder /ui/dist/bundle.js ./ui/dist/bundle.js
RUN go mod tidy && CGO_ENABLED=0 go build -ldflags="-s -w" -o /lineage .

FROM alpine:3.23@sha256:5b10f432ef3da1b8d4c7eb6c487f2f5a8f096bc91145e68878dd4a5019afde11
RUN addgroup -S rat && adduser -S -G rat rat
COPY --from=go-builder /lineage /lineage
USER rat
EXPOSE 50098
ENTRYPOINT ["/lineage"]
