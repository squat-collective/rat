# RAT MCP-docs plugin. Build from the repo root:
#
#   docker build --build-context platform=platform \
#     -f examples/rat-plugin-mcp-docs/Dockerfile \
#     -t rat-plugin-mcp-docs examples/rat-plugin-mcp-docs
#
# Or `make build` from this directory.
FROM golang:1.24-alpine AS builder
WORKDIR /build

COPY --from=platform . /platform/
COPY . .

RUN go mod tidy && CGO_ENABLED=0 go build -ldflags="-s -w" -o /mcp-docs .

FROM alpine:3.21
RUN addgroup -S rat && adduser -S -G rat rat
COPY --from=builder /mcp-docs /mcp-docs
USER rat
EXPOSE 50100
ENTRYPOINT ["/mcp-docs"]
