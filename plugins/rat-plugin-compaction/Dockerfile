FROM golang:1.26-alpine@sha256:f23e8b227fb4493eabe03bede4d5a32d04092da71962f1fb79b5f7d1e6c2a17f AS builder
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
# pyiceberg ships no cp314 wheel yet, so its avro C extension compiles from
# sdist — install build-essential just long enough to build the wheel, then
# purge it in the same layer so the final image stays slim (no compiler shipped).
# Drop the build-essential dance once pyiceberg publishes cp314 wheels.
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates build-essential \
    && pip install --no-cache-dir 'pyiceberg[pyarrow]==0.11.1' \
    && apt-get purge -y --auto-remove build-essential \
    && rm -rf /var/lib/apt/lists/*
RUN groupadd -r rat && useradd -r -g rat rat
COPY --from=builder /compaction /compaction
COPY compact.py /compact.py
USER rat
EXPOSE 50110
ENTRYPOINT ["/compaction"]
