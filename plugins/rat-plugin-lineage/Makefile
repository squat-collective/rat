.PHONY: build test run clean ui-watch

ROOT := $(shell cd ../.. && pwd)
RATD_URL ?= http://ratd:8080
RATD_INTERNAL_URL ?= http://ratd:8090

build:
	cd $(ROOT) && docker build \
		--build-context platform=platform \
		--build-context sdk=sdk-go \
		-f examples/rat-plugin-lineage/Dockerfile \
		-t rat-plugin-lineage \
		examples/rat-plugin-lineage

# Local UI watch — convenient for iterating on the React Flow components
# without rebuilding the docker image each time.
ui-watch:
	cd ui && npm install --no-audit --no-fund && node build.mjs --watch

run:
	docker rm -f lineage 2>/dev/null || true
	docker run -d --name lineage --network infra_default \
		-e RATD_URL="$(RATD_URL)" \
		-e RATD_INTERNAL_URL="$(RATD_INTERNAL_URL)" \
		rat-plugin-lineage

clean:
	docker rm -f lineage 2>/dev/null || true
	docker rmi rat-plugin-lineage 2>/dev/null || true
