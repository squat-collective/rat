# RAT lineage plugin — three-stage build:
#   1. node:20 builds the UI bundle (esbuild → dist/bundle.js)
#   2. golang:1.24 builds the Go binary (embeds the UI via go:embed)
#   3. alpine:3.21 runtime
#
# Build from the repo root so the platform module is available as a
# named context:
#
#   docker build --build-context platform=platform \
#     -f examples/rat-plugin-lineage/Dockerfile \
#     -t rat-plugin-lineage examples/rat-plugin-lineage
#
# Or `make build` from this directory.
FROM node:20-alpine@sha256:fb4cd12c85ee03686f6af5362a0b0d56d50c58a04632e6c0fb8363f609372293 AS ui-builder
WORKDIR /ui
COPY ui/package.json ui/package-lock.json* ./
RUN npm install --no-audit --no-fund --silent || npm install --no-audit --no-fund
COPY ui/ ./
RUN npm run build

FROM golang:1.24-alpine@sha256:8bee1901f1e530bfb4a7850aa7a479d17ae3a18beb6e09064ed54cfd245b7191 AS go-builder
WORKDIR /build
COPY --from=platform . /platform/
COPY . .
# Pull the built UI bundle into the Go build context so go:embed picks
# it up. ui/dist/bundle.js is the only file we need at this stage.
COPY --from=ui-builder /ui/dist/bundle.js ./ui/dist/bundle.js
RUN go mod tidy && CGO_ENABLED=0 go build -ldflags="-s -w" -o /lineage .

FROM alpine:3.21@sha256:48b0309ca019d89d40f670aa1bc06e426dc0931948452e8491e3d65087abc07d
RUN addgroup -S rat && adduser -S -G rat rat
COPY --from=go-builder /lineage /lineage
USER rat
EXPOSE 50098
ENTRYPOINT ["/lineage"]
