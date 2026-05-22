.PHONY: build test run clean

# Repo root (examples/<plugin>/ -> ../../)
ROOT := $(shell cd ../.. && pwd)

# Build the plugin image. The platform module is passed as a named build
# context so the repo-root .dockerignore does not exclude it.
build:
	cd $(ROOT) && docker build \
		--build-context platform=platform \
		-f examples/rat-plugin-event-notifier/Dockerfile \
		-t rat-plugin-event-notifier \
		examples/rat-plugin-event-notifier

# Run the Go tests (platform module mounted for the replace directive).
test:
	docker run --rm \
		-v "$(shell pwd)":/work \
		-v "$(ROOT)/platform":/platform \
		-w /work golang:1.24-alpine \
		sh -c "go mod tidy && go test ./..."

# Run the plugin on ratd's Docker network (phone-home + callbacks).
run:
	docker run -d --name event-notifier --network infra_default \
		rat-plugin-event-notifier

clean:
	docker rm -f event-notifier 2>/dev/null || true
	docker rmi rat-plugin-event-notifier 2>/dev/null || true
