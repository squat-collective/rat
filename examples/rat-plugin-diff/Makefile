.PHONY: build test run clean

ROOT := $(shell cd ../.. && pwd)
RATD_URL ?= http://ratd:8080

build:
	cd $(ROOT) && docker build \
		--build-context platform=platform \
		-f examples/rat-plugin-diff/Dockerfile \
		-t rat-plugin-diff \
		examples/rat-plugin-diff

run:
	docker rm -f diff 2>/dev/null || true
	docker run -d --name diff --network infra_default \
		-e RATD_URL="$(RATD_URL)" \
		rat-plugin-diff

clean:
	docker rm -f diff 2>/dev/null || true
	docker rmi rat-plugin-diff 2>/dev/null || true
