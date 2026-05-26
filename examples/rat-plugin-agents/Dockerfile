# RAT agents plugin. Build from the repo root:
#
#   docker build --build-context platform=platform \
#     -f examples/rat-plugin-agents/Dockerfile \
#     -t rat-plugin-agents examples/rat-plugin-agents
#
# Or `make build` from this directory.
FROM golang:1.24-alpine AS builder
WORKDIR /build

COPY --from=platform . /platform/
COPY . .

RUN go mod tidy && CGO_ENABLED=0 go build -ldflags="-s -w" -o /agents .

FROM alpine:3.21
RUN addgroup -S rat && adduser -S -G rat rat
COPY --from=builder /agents /agents
USER rat
EXPOSE 50096
ENTRYPOINT ["/agents"]
