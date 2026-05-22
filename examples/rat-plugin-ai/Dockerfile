# Example RAT AI plugin.
#
# The platform module (a local `replace` dependency) is supplied as a NAMED
# build context. Build from the repo root:
#
#   docker build --build-context platform=platform \
#     -f examples/rat-plugin-ai/Dockerfile \
#     -t rat-plugin-ai examples/rat-plugin-ai
#
# Or just run `make build` from this directory.
FROM golang:1.24-alpine AS builder
WORKDIR /build

# The platform module supplies the generated plugin/v1 ConnectRPC stubs.
COPY --from=platform . /platform/
COPY . .

RUN go mod tidy && CGO_ENABLED=0 go build -ldflags="-s -w" -o /ai .

FROM alpine:3.21
RUN addgroup -S rat && adduser -S -G rat rat
COPY --from=builder /ai /ai
USER rat
EXPOSE 50091
ENTRYPOINT ["/ai"]
