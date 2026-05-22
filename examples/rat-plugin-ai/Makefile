.PHONY: build test run clean

ROOT := $(shell cd ../.. && pwd)

# Override on the command line, e.g.
#   make run OPENAI_BASE_URL=http://my-ollama:11434/v1 AI_MODEL=llama3.1
OPENAI_BASE_URL ?= http://10.2.1.1:11434/v1
AI_MODEL        ?= gpt-oss:20b

# Build the plugin image (platform module passed as a named build context).
build:
	cd $(ROOT) && docker build \
		--build-context platform=platform \
		-f examples/rat-plugin-ai/Dockerfile \
		-t rat-plugin-ai \
		examples/rat-plugin-ai

# Run the Go tests (platform module mounted for the replace directive).
test:
	docker run --rm \
		-v "$(shell pwd)":/work \
		-v "$(ROOT)/platform":/platform \
		-w /work golang:1.24-alpine \
		sh -c "go mod tidy && go test ./..."

# Run the plugin on ratd's Docker network, pointed at an OpenAI-compatible API.
run:
	docker run -d --name ai --network infra_default \
		-e OPENAI_BASE_URL="$(OPENAI_BASE_URL)" \
		-e AI_MODEL="$(AI_MODEL)" \
		rat-plugin-ai

clean:
	docker rm -f ai 2>/dev/null || true
	docker rmi rat-plugin-ai 2>/dev/null || true
