.PHONY: build test run clean

ROOT := $(shell cd ../.. && pwd)
RATD_URL ?= http://ratd:8080
RATD_INTERNAL_URL ?= http://ratd:8090

build:
	cd $(ROOT) && docker build \
		--build-context platform=platform \
		--build-context sdk=sdk-go \
		-f plugins/rat-plugin-compaction/Dockerfile \
		-t rat-plugin-compaction \
		plugins/rat-plugin-compaction

test:
	docker run --rm \
		-v "$(shell pwd)":/work \
		-v "$(ROOT)/platform":/platform \
		-v "$(ROOT)/sdk-go":/sdk-go \
		-w /work golang:1.24-alpine \
		sh -c "go mod tidy && go test ./..."

run:
	docker rm -f compaction 2>/dev/null || true
	docker run -d --name compaction --network infra_default \
		-e RATD_URL="$(RATD_URL)" \
		-e RATD_INTERNAL_URL="$(RATD_INTERNAL_URL)" \
		-e NESSIE_URL="http://nessie:19120" \
		-e S3_ENDPOINT="http://minio:9000" \
		-e S3_ACCESS_KEY="minioadmin" \
		-e S3_SECRET_KEY="minioadmin" \
		-e S3_BUCKET="rat" \
		rat-plugin-compaction

clean:
	docker rm -f compaction 2>/dev/null || true
	docker rmi rat-plugin-compaction 2>/dev/null || true
