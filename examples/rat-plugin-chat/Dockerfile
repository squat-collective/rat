# RAT chat plugin. Build from the repo root:
#
#   docker build --build-context platform=platform \
#     -f examples/rat-plugin-chat/Dockerfile \
#     -t rat-plugin-chat examples/rat-plugin-chat
#
# Or `make build` from this directory.
FROM golang:1.24-alpine AS builder
WORKDIR /build

COPY --from=platform . /platform/
COPY . .

RUN go mod tidy && CGO_ENABLED=0 go build -ldflags="-s -w" -o /chat .

FROM alpine:3.21
RUN addgroup -S rat && adduser -S -G rat rat
COPY --from=builder /chat /chat
USER rat
EXPOSE 50095
ENTRYPOINT ["/chat"]
