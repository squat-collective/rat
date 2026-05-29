FROM golang:1.26-alpine@sha256:91eda9776261207ea25fd06b5b7fed8d397dd2c0a283e77f2ab6e91bfa71079d AS builder
WORKDIR /build

COPY --from=platform . /platform/
COPY --from=sdk . /sdk-go/
COPY . .

RUN go mod tidy && CGO_ENABLED=0 go build -ldflags="-s -w" -o /compaction .

# Python runtime stage — needs pyiceberg + pyarrow to do the actual rewrite.
# We pin pyiceberg to match what runner ships (0.11.x); pyarrow follows from
# the pyiceberg constraint. Slim image keeps the final container ~300 MB
# instead of ~1.5 GB.
FROM python:3.14-slim@sha256:c845af9399020c7e562969a13689e929074a10fd057acd1b1fad06a2fb068e97
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates \
    && rm -rf /var/lib/apt/lists/* \
    && pip install --no-cache-dir 'pyiceberg[pyarrow]==0.11.1'
RUN groupadd -r rat && useradd -r -g rat rat
COPY --from=builder /compaction /compaction
COPY compact.py /compact.py
USER rat
EXPOSE 50110
ENTRYPOINT ["/compaction"]
