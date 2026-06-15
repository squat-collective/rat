FROM golang:1.26-alpine@sha256:7a3e50096189ad57c9f9f865e7e4aa8585ed1585248513dc5cda498e2f41812c AS builder
WORKDIR /build

COPY --from=platform . /platform/
COPY --from=sdk . /sdk-go/
COPY . .

RUN go mod tidy && CGO_ENABLED=0 go build -ldflags="-s -w" -o /compaction .

# Python runtime stage — needs pyiceberg + pyarrow to do the actual rewrite.
# We pin pyiceberg to match what runner ships (0.11.x); pyarrow follows from
# the pyiceberg constraint. Slim image keeps the final container ~300 MB
# instead of ~1.5 GB.
FROM python:3.14-slim@sha256:44dd04494ee8f3b538294360e7c4b3acb87c8268e4d0a4828a6500b1eff50061
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
