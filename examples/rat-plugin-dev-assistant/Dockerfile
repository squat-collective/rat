# Example RAT dev-assistant plugin — a platform + portal plugin.
#
# The portal UI bundle is hand-written and build-free, so there is no
# front-end build step — just Go.
#
# Build from the repo root (the platform module is a named build context):
#
#   docker build --build-context platform=platform \
#     -f examples/rat-plugin-dev-assistant/Dockerfile \
#     -t rat-plugin-dev-assistant examples/rat-plugin-dev-assistant
#
# Or just run `make build` from this directory.
FROM golang:1.24-alpine AS builder
WORKDIR /build

COPY --from=platform . /platform/
COPY . .

RUN go mod tidy && CGO_ENABLED=0 go build -ldflags="-s -w" -o /dev-assistant .

FROM alpine:3.21
RUN addgroup -S rat && adduser -S -G rat rat
COPY --from=builder /dev-assistant /dev-assistant
USER rat
EXPOSE 50095
ENTRYPOINT ["/dev-assistant"]
