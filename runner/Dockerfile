# Build stage — only production dependencies
FROM python:3.12-slim@sha256:090ba77e2958f6af52a5341f788b50b032dd4ca28377d2893dcf1ecbdfdfe203 AS builder
WORKDIR /build
RUN pip install --no-cache-dir uv
# Copy the lockfile + pyproject so uv can install from the frozen set
# BEFORE we copy the source — keeps the dep layer cacheable across source changes.
COPY pyproject.toml uv.lock ./
COPY src/ ./src/
# `uv sync --locked` asserts that uv.lock is in sync with pyproject.toml and
# fails the build if the lockfile is stale (drift between the two files).
# Note: `--frozen` would SILENTLY skip lock resolution; `--locked` is the
# correct strict-mode flag for reproducible CI/Docker builds.
# `--no-dev` skips dev-only deps for the production image.
# `--no-editable` builds rat_runner as a regular wheel into the venv's
# site-packages — without this, an editable install leaves a .pth file
# pointing at /build/src/rat_runner which no longer exists in the runtime
# stage (we don't COPY the source into /app anymore).
RUN uv sync --locked --no-dev --no-editable

# Runtime stage
FROM python:3.12-slim@sha256:090ba77e2958f6af52a5341f788b50b032dd4ca28377d2893dcf1ecbdfdfe203
LABEL org.opencontainers.image.title="rat-runner"
LABEL org.opencontainers.image.description="RAT pipeline runner (Python execution engine)"
LABEL org.opencontainers.image.source="https://github.com/squat-collective/rat"
LABEL org.opencontainers.image.licenses="AGPL-3.0-only"
LABEL org.opencontainers.image.vendor="RAT Data"
RUN useradd --create-home runner
WORKDIR /app
# Copy the uv-managed virtualenv from the builder stage.
# The rat_runner package is installed inside the venv (not editable),
# so we don't need to copy /app/src separately.
COPY --from=builder /build/.venv /app/.venv
ENV PATH="/app/.venv/bin:$PATH"
USER runner
EXPOSE 50052
HEALTHCHECK --interval=10s --timeout=5s --retries=5 --start-period=15s \
  CMD ["python", "-c", "import grpc; ch=grpc.insecure_channel('localhost:50052'); grpc.channel_ready_future(ch).result(timeout=2)"]
ENTRYPOINT ["python", "-m", "rat_runner"]
