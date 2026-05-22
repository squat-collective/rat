.PHONY: build ui test run clean

ROOT := $(shell cd ../.. && pwd)

# Override on the command line, e.g. make run RATD_URL=http://ratd:8080
RATD_URL ?= http://ratd:8080

# Build the plugin image (platform module passed as a named build context).
# The Dockerfile builds the UI bundle internally — no need to run `make ui`.
build:
	cd $(ROOT) && docker build \
		--build-context platform=platform \
		-f examples/rat-plugin-charts/Dockerfile \
		-t rat-plugin-charts \
		examples/rat-plugin-charts

# Regenerate the committed portal UI bundle (esbuild + Recharts) in a node
# container. Run this after editing anything under ui/ so `go test` and the
# IDE see the current bundle; `make build` rebuilds it regardless.
ui:
	docker run --rm -v "$(shell pwd)/ui":/ui -w /ui node:20-alpine \
		sh -c "npm install --no-audit --no-fund && npm run build"
	cp ui/dist/bundle.js bundle.js

# Run the Go tests (platform module mounted for the replace directive).
test:
	docker run --rm \
		-v "$(shell pwd)":/work \
		-v "$(ROOT)/platform":/platform \
		-w /work golang:1.24-alpine \
		sh -c "go mod tidy && go test ./..."

# Run the plugin on ratd's Docker network.
run:
	docker run -d --name charts --network infra_default \
		-e RATD_URL="$(RATD_URL)" \
		rat-plugin-charts

clean:
	docker rm -f charts 2>/dev/null || true
	docker rmi rat-plugin-charts 2>/dev/null || true
