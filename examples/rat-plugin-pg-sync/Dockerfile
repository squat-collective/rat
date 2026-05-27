FROM golang:1.24-alpine AS builder
WORKDIR /build

COPY --from=platform . /platform/
COPY --from=sdk . /sdk-go/
COPY . .

RUN go mod tidy && CGO_ENABLED=0 go build -ldflags="-s -w" -o /pg-sync .

FROM alpine:3.21
RUN addgroup -S rat && adduser -S -G rat rat
COPY --from=builder /pg-sync /pg-sync
USER rat
EXPOSE 50100
ENTRYPOINT ["/pg-sync"]
