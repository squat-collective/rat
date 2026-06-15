# Example RAT demo-loader plugin — a platform + portal plugin.
#
# The portal UI bundle is build-free.
#
# Build from the repo root:
#   docker build \
#     --build-context platform=platform \
#     --build-context sdk=sdk-go \
#     -f examples/rat-plugin-demo-loader/Dockerfile \
#     -t rat-plugin-demo-loader examples/rat-plugin-demo-loader
#
# Or just run `make build` from this directory.
FROM golang:1.26-alpine@sha256:7a3e50096189ad57c9f9f865e7e4aa8585ed1585248513dc5cda498e2f41812c AS builder
WORKDIR /build

COPY --from=platform . /platform/
COPY --from=sdk . /sdk-go/
COPY . .

RUN go mod tidy && CGO_ENABLED=0 go build -ldflags="-s -w" -o /demo-loader .

FROM alpine:3.24@sha256:a2d49ea686c2adfe3c992e47dc3b5e7fa6e6b5055609400dc2acaeb241c829f4
RUN addgroup -S rat && adduser -S -G rat rat
COPY --from=builder /demo-loader /demo-loader
USER rat
EXPOSE 50097
ENTRYPOINT ["/demo-loader"]
