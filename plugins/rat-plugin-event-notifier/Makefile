.PHONY: build test run clean

# Repo root (examples/<plugin>/ -> ../../)
ROOT := $(shell cd ../.. && pwd)

RATD_URL ?= http://ratd:8080
RATD_INTERNAL_URL ?= http://ratd:8090

# Build the plugin image. The platform module is passed as a named build
# context so the repo-root .dockerignore does not exclude it.
build:
	cd $(ROOT) && docker build \
		--build-context platform=platform \
		--build-context sdk=sdk-go \
		-f plugins/rat-plugin-event-notifier/Dockerfile \
		-t rat-plugin-event-notifier \
		plugins/rat-plugin-event-notifier

# Run the Go tests (platform module mounted for the replace directive).
test:
	docker run --rm \
		-v "$(shell pwd)":/work \
		-v "$(ROOT)/platform":/platform \
		-v "$(ROOT)/sdk-go":/sdk-go \
		-w /work golang:1.26-alpine \
		sh -c "go mod tidy && go test ./..."

# Run the plugin on ratd's Docker network (phone-home + callbacks).
run:
	docker run -d --name event-notifier --network infra_default \
		-e RATD_URL="$(RATD_URL)" \
		-e RATD_INTERNAL_URL="$(RATD_INTERNAL_URL)" \
		rat-plugin-event-notifier

clean:
	docker rm -f event-notifier 2>/dev/null || true
	docker rmi rat-plugin-event-notifier 2>/dev/null || true
