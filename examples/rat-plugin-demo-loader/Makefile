.PHONY: build test run clean

ROOT := $(shell cd ../.. && pwd)

RATD_URL ?= http://ratd:8080

build:
	cd $(ROOT) && docker build \
		--build-context platform=platform \
		-f examples/rat-plugin-demo-loader/Dockerfile \
		-t rat-plugin-demo-loader \
		examples/rat-plugin-demo-loader

test:
	docker run --rm \
		-v "$(shell pwd)":/work \
		-v "$(ROOT)/platform":/platform \
		-w /work golang:1.24-alpine \
		sh -c "go mod tidy && go test ./..."

run:
	docker run -d --name demo-loader --network infra_default \
		-e RATD_URL="$(RATD_URL)" \
		rat-plugin-demo-loader

clean:
	docker rm -f demo-loader 2>/dev/null || true
	docker rmi rat-plugin-demo-loader 2>/dev/null || true
