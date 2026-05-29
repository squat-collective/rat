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
FROM golang:1.26-alpine@sha256:91eda9776261207ea25fd06b5b7fed8d397dd2c0a283e77f2ab6e91bfa71079d AS builder
WORKDIR /build

COPY --from=platform . /platform/
COPY --from=sdk . /sdk-go/
COPY . .

RUN go mod tidy && CGO_ENABLED=0 go build -ldflags="-s -w" -o /docs-assistant .

FROM alpine:3.23@sha256:5b10f432ef3da1b8d4c7eb6c487f2f5a8f096bc91145e68878dd4a5019afde11
RUN addgroup -S rat && adduser -S -G rat rat
COPY --from=builder /docs-assistant /docs-assistant
USER rat
EXPOSE 50096
ENTRYPOINT ["/docs-assistant"]
