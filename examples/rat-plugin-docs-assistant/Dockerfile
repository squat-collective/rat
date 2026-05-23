# Example RAT docs-assistant plugin — a platform + portal plugin.
#
# The portal UI bundle is build-free, so there is no front-end build step.
#
# Build from the repo root (the platform module is a named build context):
#
#   docker build --build-context platform=platform \
#     -f examples/rat-plugin-docs-assistant/Dockerfile \
#     -t rat-plugin-docs-assistant examples/rat-plugin-docs-assistant
#
# Or just run `make build` from this directory.
FROM golang:1.24-alpine AS builder
WORKDIR /build

COPY --from=platform . /platform/
COPY . .

RUN go mod tidy && CGO_ENABLED=0 go build -ldflags="-s -w" -o /docs-assistant .

FROM alpine:3.21
RUN addgroup -S rat && adduser -S -G rat rat
COPY --from=builder /docs-assistant /docs-assistant
USER rat
EXPOSE 50096
ENTRYPOINT ["/docs-assistant"]
