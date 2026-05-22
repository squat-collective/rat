.PHONY: build test run clean

ROOT := $(shell cd ../.. && pwd)

# Override on the command line, e.g. make run RATD_URL=http://ratd:8080
RATD_URL ?= http://ratd:8080

# Build the plugin image (platform module passed as a named build context).
build:
	cd $(ROOT) && docker build \
		--build-context platform=platform \
		-f examples/rat-plugin-interconnect/Dockerfile \
		-t rat-plugin-interconnect \
		examples/rat-plugin-interconnect

# Run the Go tests (platform module mounted for the replace directive).
test:
	docker run --rm \
		-v "$(shell pwd)":/work \
		-v "$(ROOT)/platform":/platform \
		-w /work golang:1.24-alpine \
		sh -c "go mod tidy && go test ./..."

# Run the plugin on ratd's Docker network.
run:
	docker run -d --name interconnect --network infra_default \
		-e RATD_URL="$(RATD_URL)" \
		rat-plugin-interconnect

clean:
	docker rm -f interconnect 2>/dev/null || true
	docker rmi rat-plugin-interconnect 2>/dev/null || true
