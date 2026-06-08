# Example RAT dev-assistant plugin — a platform + portal plugin.
#
# The portal UI bundle is hand-written and build-free, so there is no
# front-end build step — just Go.
#
# Build from the repo root (the platform module is a named build context):
#
#   docker build \
#     --build-context platform=platform \
#     --build-context sdk=sdk-go \
#     -f examples/rat-plugin-dev-assistant/Dockerfile \
#     -t rat-plugin-dev-assistant examples/rat-plugin-dev-assistant
#
# Or just run `make build` from this directory.
FROM golang:1.26-alpine@sha256:f23e8b227fb4493eabe03bede4d5a32d04092da71962f1fb79b5f7d1e6c2a17f AS builder
WORKDIR /build

COPY --from=platform . /platform/
COPY --from=sdk . /sdk-go/
COPY . .

RUN go mod tidy && CGO_ENABLED=0 go build -ldflags="-s -w" -o /dev-assistant .

FROM alpine:3.23@sha256:5b10f432ef3da1b8d4c7eb6c487f2f5a8f096bc91145e68878dd4a5019afde11
RUN addgroup -S rat && adduser -S -G rat rat
COPY --from=builder /dev-assistant /dev-assistant
USER rat
EXPOSE 50095
ENTRYPOINT ["/dev-assistant"]
