# Local checks mirroring .github/workflows/pr-checks.yml (Go job + compile).
# Docker images: make docker
#
# test uses -p 1 so packages with AP_TEST_DATABASE_URL integration tests do not
# hit the same Postgres concurrently (matches CI). Mastodon/AP integration tests
# use Postgres for persistence and the SQL job queue (queue_jobs) like production.

.PHONY: default build test vet fmt-check fmt mod-verify docker apd-image apw-image

default: build

# Everything needed before opening a PR (fmt, modules, vet, tests, compile).
build: fmt-check mod-verify vet test
	go build ./...

test:
	go test -race -p 1 -v ./...

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
