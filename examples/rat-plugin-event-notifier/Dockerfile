# Example RAT platform plugin.
#
# The platform module (a local `replace` dependency) is supplied as a NAMED
# build context, so the build does not depend on the repo-root .dockerignore.
# Build from the repo root:
#
#   docker build --build-context platform=platform \
#     -f examples/rat-plugin-event-notifier/Dockerfile \
#     -t rat-plugin-event-notifier examples/rat-plugin-event-notifier
#
# Or just run `make build` from this directory.
FROM golang:1.24-alpine AS builder
WORKDIR /build

# The platform module supplies the generated plugin/v1 ConnectRPC stubs.
# go.mod's `replace ... => ../../platform` resolves to /platform from /build.
COPY --from=platform . /platform/
COPY . .

RUN go mod tidy && CGO_ENABLED=0 go build -ldflags="-s -w" -o /event-notifier .

FROM alpine:3.21
RUN addgroup -S rat && adduser -S -G rat rat
COPY --from=builder /event-notifier /event-notifier
USER rat
EXPOSE 50090
ENTRYPOINT ["/event-notifier"]
