# RAT chat plugin. Build from the repo root:
#
#   docker build \
#     --build-context platform=platform \
#     --build-context sdk=sdk-go \
#     -f examples/rat-plugin-chat/Dockerfile \
#     -t rat-plugin-chat examples/rat-plugin-chat
#
# Or `make build` from this directory.
FROM golang:1.24-alpine@sha256:8bee1901f1e530bfb4a7850aa7a479d17ae3a18beb6e09064ed54cfd245b7191 AS builder
WORKDIR /build

COPY --from=platform . /platform/
COPY --from=sdk . /sdk-go/
COPY . .

RUN go mod tidy && CGO_ENABLED=0 go build -ldflags="-s -w" -o /chat .

FROM alpine:3.21@sha256:48b0309ca019d89d40f670aa1bc06e426dc0931948452e8491e3d65087abc07d
RUN addgroup -S rat && adduser -S -G rat rat \
    && mkdir -p /data/conversations \
    && chown -R rat:rat /data
COPY --from=builder /chat /chat
USER rat
VOLUME ["/data"]
EXPOSE 50095
ENTRYPOINT ["/chat"]
