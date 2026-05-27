.PHONY: build test run clean

ROOT := $(shell cd ../.. && pwd)

# Override on the command line, e.g.
#   make run OPENAI_BASE_URL=http://10.2.1.1:11434/v1 AI_MODEL=gpt-oss:20b
RATD_URL          ?= http://ratd:8080
RATD_INTERNAL_URL ?= http://ratd:8090
OPENAI_BASE_URL ?= http://10.2.1.1:11434/v1
AI_MODEL        ?= gpt-oss:20b

# Build the plugin image (platform module passed as a named build context).
build:
	cd $(ROOT) && docker build \
		--build-context platform=platform \
		--build-context sdk=sdk-go \
		-f examples/rat-plugin-ai-provider/Dockerfile \
		-t rat-plugin-ai-provider \
		examples/rat-plugin-ai-provider

# Run the Go tests (platform module mounted for the replace directive).
test:
	docker run --rm \
		-v "$(shell pwd)":/work \
		-v "$(ROOT)/platform":/platform \
		-v "$(ROOT)/sdk-go":/sdk-go \
		-w /work golang:1.24-alpine \
		sh -c "go mod tidy && go test ./..."

# Run the plugin on ratd's Docker network. The OpenAI settings here are only
# the initial defaults — change them live in the portal's plugin settings.
run:
	docker run -d --name ai-provider --network infra_default \
		-e RATD_URL="$(RATD_URL)" \
		-e RATD_INTERNAL_URL="$(RATD_INTERNAL_URL)" \
		-e OPENAI_BASE_URL="$(OPENAI_BASE_URL)" \
		-e AI_MODEL="$(AI_MODEL)" \
		rat-plugin-ai-provider

clean:
	docker rm -f ai-provider 2>/dev/null || true
	docker rmi rat-plugin-ai-provider 2>/dev/null || true
