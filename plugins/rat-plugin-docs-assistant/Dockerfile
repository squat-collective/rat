# Example RAT docs-assistant plugin — a platform + portal plugin.
#
# The portal UI bundle is build-free, so there is no front-end build step.
#
# Build from the repo root (the platform module is a named build context):
#
#   docker build \
#     --build-context platform=platform \
#     --build-context sdk=sdk-go \
#     -f examples/rat-plugin-docs-assistant/Dockerfile \
#     -t rat-plugin-docs-assistant examples/rat-plugin-docs-assistant
#
# Or just run `make build` from this directory.
FROM golang:1.26-alpine@sha256:7a3e50096189ad57c9f9f865e7e4aa8585ed1585248513dc5cda498e2f41812c AS builder
WORKDIR /build

COPY --from=platform . /platform/
COPY --from=sdk . /sdk-go/
COPY . .

RUN go mod tidy && CGO_ENABLED=0 go build -ldflags="-s -w" -o /docs-assistant .

FROM alpine:3.24@sha256:a2d49ea686c2adfe3c992e47dc3b5e7fa6e6b5055609400dc2acaeb241c829f4
RUN addgroup -S rat && adduser -S -G rat rat
COPY --from=builder /docs-assistant /docs-assistant
USER rat
EXPOSE 50096
ENTRYPOINT ["/docs-assistant"]
