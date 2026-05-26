.PHONY: build test run clean

ROOT := $(shell cd ../.. && pwd)
RATD_URL ?= http://ratd:8080
RATD_INTERNAL_URL ?= http://ratd:8090

build:
	cd $(ROOT) && docker build \
		--build-context platform=platform \
		-f examples/rat-plugin-chat/Dockerfile \
		-t rat-plugin-chat \
		examples/rat-plugin-chat

test:
	docker run --rm \
		-v "$(shell pwd)":/work \
		-v "$(ROOT)/platform":/platform \
		-w /work golang:1.24-alpine \
		sh -c "go mod tidy && go test ./..."

run:
	docker rm -f chat 2>/dev/null || true
	docker run -d --name chat --network infra_default \
		-e RATD_URL="$(RATD_URL)" \
		-e RATD_INTERNAL_URL="$(RATD_INTERNAL_URL)" \
		-v rat-chat-history:/data \
		rat-plugin-chat

clean:
	docker rm -f chat 2>/dev/null || true
	docker rmi rat-plugin-chat 2>/dev/null || true

# Wipe persisted conversations (volume + container).
clean-history:
	docker rm -f chat 2>/dev/null || true
	docker volume rm rat-chat-history 2>/dev/null || true
