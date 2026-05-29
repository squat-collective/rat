# Build stage — production dependencies only.
FROM python:3.14-slim@sha256:c845af9399020c7e562969a13689e929074a10fd057acd1b1fad06a2fb068e97 AS builder
WORKDIR /build
# build-essential: pyiceberg ships no cp314 wheel yet, so its avro C extension
# compiles from sdist during `uv sync`. Builder-stage only — the slim runtime
# just copies the resulting .venv, so no compiler is shipped. Drop once cp314
# wheels land.
RUN apt-get update \
  && apt-get install -y --no-install-recommends build-essential \
  && rm -rf /var/lib/apt/lists/*
RUN pip install --no-cache-dir uv
# Copy lockfile + pyproject first so the dep layer caches across source changes.
COPY pyproject.toml uv.lock ./
COPY src/ ./src/
# `uv sync --locked` asserts uv.lock is in sync with pyproject.toml (fails on
# drift). --no-dev skips dev deps; --no-editable installs the package as a wheel
# (incl. the generated gen/ stubs) so the runtime stage needs no source copy.
RUN uv sync --locked --no-dev --no-editable

# Runtime stage.
FROM python:3.14-slim@sha256:c845af9399020c7e562969a13689e929074a10fd057acd1b1fad06a2fb068e97
LABEL org.opencontainers.image.title="rat-warehouse-iceberg-nessie"
LABEL org.opencontainers.image.description="RAT reference warehouse — Iceberg on S3/MinIO + Nessie, serving warehouse/v1 (ADR-024)"
LABEL org.opencontainers.image.source="https://github.com/squat-collective/rat"
LABEL org.opencontainers.image.licenses="Apache-2.0"
LABEL org.opencontainers.image.vendor="RAT Data"
RUN useradd --create-home warehouse
WORKDIR /app
COPY --from=builder /build/.venv /app/.venv
ENV PATH="/app/.venv/bin:$PATH"
USER warehouse
EXPOSE 50080
HEALTHCHECK --interval=10s --timeout=5s --retries=5 --start-period=15s \
  CMD ["python", "-c", "import grpc; ch=grpc.insecure_channel('localhost:50080'); grpc.channel_ready_future(ch).result(timeout=2)"]
ENTRYPOINT ["python", "-m", "rat_warehouse_iceberg"]
