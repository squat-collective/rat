# RAT agents plugin. Build from the repo root:
#
#   docker build --build-context platform=platform \
#     -f examples/rat-plugin-agents/Dockerfile \
#     -t rat-plugin-agents examples/rat-plugin-agents
#
# Or `make build` from this directory.
FROM golang:1.24-alpine@sha256:8bee1901f1e530bfb4a7850aa7a479d17ae3a18beb6e09064ed54cfd245b7191 AS builder
WORKDIR /build

COPY --from=platform . /platform/
COPY . .

RUN go mod tidy && CGO_ENABLED=0 go build -ldflags="-s -w" -o /agents .

FROM alpine:3.21@sha256:48b0309ca019d89d40f670aa1bc06e426dc0931948452e8491e3d65087abc07d
RUN addgroup -S rat && adduser -S -G rat rat
COPY --from=builder /agents /agents
USER rat
EXPOSE 50096
ENTRYPOINT ["/agents"]
