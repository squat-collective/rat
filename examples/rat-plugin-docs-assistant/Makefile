.PHONY: build test run clean

ROOT := $(shell cd ../.. && pwd)

RATD_URL ?= http://ratd:8080

build:
	cd $(ROOT) && docker build \
		--build-context platform=platform \
		-f examples/rat-plugin-docs-assistant/Dockerfile \
		-t rat-plugin-docs-assistant \
		examples/rat-plugin-docs-assistant

test:
	docker run --rm \
		-v "$(shell pwd)":/work \
		-v "$(ROOT)/platform":/platform \
		-w /work golang:1.24-alpine \
		sh -c "go mod tidy && go test ./..."

run:
	docker run -d --name docs-assistant --network infra_default \
		-e RATD_URL="$(RATD_URL)" \
		rat-plugin-docs-assistant

clean:
	docker rm -f docs-assistant 2>/dev/null || true
	docker rmi rat-plugin-docs-assistant 2>/dev/null || true
