# Example RAT demo-loader plugin — a platform + portal plugin.
#
# The portal UI bundle is build-free.
#
# Build from the repo root:
#   docker build --build-context platform=platform \
#     -f examples/rat-plugin-demo-loader/Dockerfile \
#     -t rat-plugin-demo-loader examples/rat-plugin-demo-loader
#
# Or just run `make build` from this directory.
FROM golang:1.24-alpine AS builder
WORKDIR /build

COPY --from=platform . /platform/
COPY . .

RUN go mod tidy && CGO_ENABLED=0 go build -ldflags="-s -w" -o /demo-loader .

FROM alpine:3.21
RUN addgroup -S rat && adduser -S -G rat rat
COPY --from=builder /demo-loader /demo-loader
USER rat
EXPOSE 50097
ENTRYPOINT ["/demo-loader"]
