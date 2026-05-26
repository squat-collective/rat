# RAT MCP-sql plugin. Build from the repo root:
#
#   docker build --build-context platform=platform \
#     -f examples/rat-plugin-mcp-sql/Dockerfile \
#     -t rat-plugin-mcp-sql examples/rat-plugin-mcp-sql
#
# Or `make build` from this directory.
FROM golang:1.24-alpine AS builder
WORKDIR /build

COPY --from=platform . /platform/
COPY . .

RUN go mod tidy && CGO_ENABLED=0 go build -ldflags="-s -w" -o /mcp-sql .

FROM alpine:3.21
RUN addgroup -S rat && adduser -S -G rat rat
COPY --from=builder /mcp-sql /mcp-sql
USER rat
EXPOSE 50101
ENTRYPOINT ["/mcp-sql"]
