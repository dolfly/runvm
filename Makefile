VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: build build-plain dist test vet fmt fmt-check doctor clean

## build: the real thing, with golem's unicorn engine compiled in
build:
	CGO_ENABLED=0 go build -tags unicorn -trimpath -ldflags "$(LDFLAGS)" -o bin/runvm ./cmd/runvm

## build-plain: no engine, for a fast compile/syntax check (android_* will report it is unavailable)
build-plain:
	CGO_ENABLED=0 go build -trimpath -o bin/runvm ./cmd/runvm

## test: unit tests; none of them boot an emulator or a browser
test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -w .

fmt-check:
	@test -z "$$(gofmt -l .)" || { gofmt -l .; echo "gofmt needed"; exit 1; }

## dist: cross-compiled archives for every supported platform
dist:
	./scripts/release.sh $(VERSION)

## doctor: probe this machine by actually starting both backends
doctor: build
	./bin/runvm doctor

clean:
	rm -rf bin dist
