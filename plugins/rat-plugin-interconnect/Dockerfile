# Example RAT interconnect plugin — a platform + portal plugin.
#
# The portal UI bundle is hand-written and build-free (plain JS using the
# portal's window.React), so there is no front-end build step — just Go.
#
# The platform module (a local `replace` dependency) is supplied as a NAMED
# build context. Build from the repo root:
#
#   docker build \
#     --build-context platform=platform \
#     --build-context sdk=sdk-go \
#     -f examples/rat-plugin-interconnect/Dockerfile \
#     -t rat-plugin-interconnect examples/rat-plugin-interconnect
#
# Or just run `make build` from this directory.
FROM golang:1.26-alpine@sha256:0178a641fbb4858c5f1b48e34bdaabe0350a330a1b1149aabd498d0699ff5fb2 AS builder
WORKDIR /build

# The platform module supplies the generated plugin/v1 ConnectRPC stubs.
COPY --from=platform . /platform/
COPY --from=sdk . /sdk-go/
COPY . .

RUN go mod tidy && CGO_ENABLED=0 go build -ldflags="-s -w" -o /interconnect .

FROM alpine:3.24@sha256:28bd5fe8b56d1bd048e5babf5b10710ebe0bae67db86916198a6eec434943f8b
RUN addgroup -S rat && adduser -S -G rat rat
COPY --from=builder /interconnect /interconnect
USER rat
EXPOSE 50093
ENTRYPOINT ["/interconnect"]
