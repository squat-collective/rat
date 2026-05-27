# Example RAT interconnect plugin — a platform + portal plugin.
#
# The portal UI bundle is hand-written and build-free (plain JS using the
# portal's window.React), so there is no front-end build step — just Go.
#
# The platform module (a local `replace` dependency) is supplied as a NAMED
# build context. Build from the repo root:
#
#   docker build --build-context platform=platform \
#     -f examples/rat-plugin-interconnect/Dockerfile \
#     -t rat-plugin-interconnect examples/rat-plugin-interconnect
#
# Or just run `make build` from this directory.
FROM golang:1.24-alpine@sha256:8bee1901f1e530bfb4a7850aa7a479d17ae3a18beb6e09064ed54cfd245b7191 AS builder
WORKDIR /build

# The platform module supplies the generated plugin/v1 ConnectRPC stubs.
COPY --from=platform . /platform/
COPY . .

RUN go mod tidy && CGO_ENABLED=0 go build -ldflags="-s -w" -o /interconnect .

FROM alpine:3.21@sha256:48b0309ca019d89d40f670aa1bc06e426dc0931948452e8491e3d65087abc07d
RUN addgroup -S rat && adduser -S -G rat rat
COPY --from=builder /interconnect /interconnect
USER rat
EXPOSE 50093
ENTRYPOINT ["/interconnect"]
