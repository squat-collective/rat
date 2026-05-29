.PHONY: build test run clean

ROOT := $(shell cd ../.. && pwd)

RATD_URL ?= http://ratd:8080
RATD_INTERNAL_URL ?= http://ratd:8090

build:
	cd $(ROOT) && docker build \
		--build-context platform=platform \
		--build-context sdk=sdk-go \
		-f plugins/rat-plugin-demo-loader/Dockerfile \
		-t rat-plugin-demo-loader \
		plugins/rat-plugin-demo-loader

test:
	docker run --rm \
		-v "$(shell pwd)":/work \
		-v "$(ROOT)/platform":/platform \
		-v "$(ROOT)/sdk-go":/sdk-go \
		-w /work golang:1.26-alpine \
		sh -c "go mod tidy && go test ./..."

run:
	docker run -d --name demo-loader --network infra_default \
		-e RATD_URL="$(RATD_URL)" \
		-e RATD_INTERNAL_URL="$(RATD_INTERNAL_URL)" \
		rat-plugin-demo-loader

clean:
	docker rm -f demo-loader 2>/dev/null || true
	docker rmi rat-plugin-demo-loader 2>/dev/null || true
