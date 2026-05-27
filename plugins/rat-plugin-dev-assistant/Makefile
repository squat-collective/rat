.PHONY: build test run clean

ROOT := $(shell cd ../.. && pwd)

RATD_URL ?= http://ratd:8080
RATD_INTERNAL_URL ?= http://ratd:8090

# Build the plugin image (platform module passed as a named build context).
build:
	cd $(ROOT) && docker build \
		--build-context platform=platform \
		--build-context sdk=sdk-go \
		-f plugins/rat-plugin-dev-assistant/Dockerfile \
		-t rat-plugin-dev-assistant \
		plugins/rat-plugin-dev-assistant

# Run the Go tests (platform module mounted for the replace directive).
test:
	docker run --rm \
		-v "$(shell pwd)":/work \
		-v "$(ROOT)/platform":/platform \
		-v "$(ROOT)/sdk-go":/sdk-go \
		-w /work golang:1.24-alpine \
		sh -c "go mod tidy && go test ./..."

# Run the plugin on ratd's Docker network.
run:
	docker run -d --name dev-assistant --network infra_default \
		-e RATD_URL="$(RATD_URL)" \
		-e RATD_INTERNAL_URL="$(RATD_INTERNAL_URL)" \
		rat-plugin-dev-assistant

clean:
	docker rm -f dev-assistant 2>/dev/null || true
	docker rmi rat-plugin-dev-assistant 2>/dev/null || true
