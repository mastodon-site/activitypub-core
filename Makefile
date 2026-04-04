# Local checks mirroring .github/workflows/pr-checks.yml (Go job + compile).
# Docker images: make docker

.PHONY: default build test vet fmt-check fmt mod-verify docker apd-image apw-image

default: build

# Everything needed before opening a PR (fmt, modules, vet, tests, compile).
build: fmt-check mod-verify vet test
	go build ./...

test:
	go test -race -v ./...

vet:
	go vet ./...

fmt-check:
	@if [ "$$(gofmt -s -l . | wc -l | tr -d ' ')" -gt 0 ]; then              \
		echo "The following files are not formatted (run: make fmt):";       \
		gofmt -s -l .;                                                         \
		exit 1;                                                                \
	fi

fmt:
	gofmt -s -w .
	go mod tidy

mod-verify:
	go mod verify

docker: apd-image apw-image

apd-image:
	docker build --target apd -t activitypub-core-apd:test .

apw-image:
	docker build --target apw -t activitypub-core-apw:test .
