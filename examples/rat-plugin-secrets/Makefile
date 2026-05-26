.PHONY: build test run clean

ROOT := $(shell cd ../.. && pwd)
RATD_URL ?= http://ratd:8080

build:
	cd $(ROOT) && docker build \
		--build-context platform=platform \
		-f examples/rat-plugin-secrets/Dockerfile \
		-t rat-plugin-secrets \
		examples/rat-plugin-secrets

test:
	docker run --rm \
		-v "$(shell pwd)":/work \
		-v "$(ROOT)/platform":/platform \
		-w /work golang:1.24-alpine \
		sh -c "go mod tidy && go test ./..."

run:
	docker rm -f secrets 2>/dev/null || true
	docker run -d --name secrets --network infra_default \
		-e RATD_URL="$(RATD_URL)" \
		-v rat-secrets-key:/data \
		rat-plugin-secrets

clean:
	docker rm -f secrets 2>/dev/null || true
	docker rmi rat-plugin-secrets 2>/dev/null || true
