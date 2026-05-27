.PHONY: build test run clean

ROOT := $(shell cd ../.. && pwd)
RATD_URL ?= http://ratd:8080
RATD_INTERNAL_URL ?= http://ratd:8090

build:
	cd $(ROOT) && docker build \
		--build-context platform=platform \
		--build-context sdk=sdk-go \
		-f plugins/rat-plugin-mcp-docs/Dockerfile \
		-t rat-plugin-mcp-docs \
		plugins/rat-plugin-mcp-docs

test:
	docker run --rm \
		-v "$(shell pwd)":/work \
		-v "$(ROOT)/platform":/platform \
		-v "$(ROOT)/sdk-go":/sdk-go \
		-w /work golang:1.24-alpine \
		sh -c "go mod tidy && go test ./..."

run:
	docker rm -f mcp-docs 2>/dev/null || true
	docker run -d --name mcp-docs --network infra_default \
		-e RATD_URL="$(RATD_URL)" \
		-e RATD_INTERNAL_URL="$(RATD_INTERNAL_URL)" \
		rat-plugin-mcp-docs

clean:
	docker rm -f mcp-docs 2>/dev/null || true
	docker rmi rat-plugin-mcp-docs 2>/dev/null || true
