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
FROM golang:1.24-alpine@sha256:8bee1901f1e530bfb4a7850aa7a479d17ae3a18beb6e09064ed54cfd245b7191 AS builder
WORKDIR /build

COPY --from=platform . /platform/
COPY --from=sdk . /sdk-go/
COPY . .

RUN go mod tidy && CGO_ENABLED=0 go build -ldflags="-s -w" -o /dev-assistant .

FROM alpine:3.21@sha256:48b0309ca019d89d40f670aa1bc06e426dc0931948452e8491e3d65087abc07d
RUN addgroup -S rat && adduser -S -G rat rat
COPY --from=builder /dev-assistant /dev-assistant
USER rat
EXPOSE 50095
ENTRYPOINT ["/dev-assistant"]
