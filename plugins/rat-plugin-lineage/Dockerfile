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
FROM node:26-alpine@sha256:7c6af15abe4e3de859690e7db171d0d711bf37d27528eddfe625b2fe89e097f8 AS ui-builder
WORKDIR /ui
COPY ui/package.json ui/package-lock.json* ./
RUN npm install --no-audit --no-fund --silent || npm install --no-audit --no-fund
COPY ui/ ./
RUN npm run build

FROM golang:1.26-alpine@sha256:91eda9776261207ea25fd06b5b7fed8d397dd2c0a283e77f2ab6e91bfa71079d AS go-builder
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
