.PHONY: build test run clean

ROOT := $(shell cd ../.. && pwd)
RATD_URL ?= http://ratd:8080
RATD_INTERNAL_URL ?= http://ratd:8090

build:
	cd $(ROOT) && docker build \
		--build-context platform=platform \
		-f examples/rat-plugin-pg-sync/Dockerfile \
		-t rat-plugin-pg-sync \
		examples/rat-plugin-pg-sync

test:
	docker run --rm \
		-v "$(shell pwd)":/work \
		-v "$(ROOT)/platform":/platform \
		-w /work golang:1.24-alpine \
		sh -c "go mod tidy && go test ./..."

run:
	docker rm -f pg-sync 2>/dev/null || true
	docker run -d --name pg-sync --network infra_default \
		-e RATD_URL="$(RATD_URL)" \
		-e RATD_INTERNAL_URL="$(RATD_INTERNAL_URL)" \
		rat-plugin-pg-sync

clean:
	docker rm -f pg-sync 2>/dev/null || true
	docker rmi rat-plugin-pg-sync 2>/dev/null || true
