GOLANGCI_VERSION ?= v2.13.2

.PHONY: help mod-verify fmt vet lint staticcheck build test test-race tidy clean all

help:
	@echo "Targets:"
	@echo "  mod-verify   go mod verify"
	@echo "  tidy         go mod tidy"
	@echo "  fmt          gofmt + goimports if present"
	@echo "  vet          go vet ./..."
	@echo "  lint         golangci-lint via go run ($(GOLANGCI_VERSION))"
	@echo "  staticcheck  staticcheck ./..."
	@echo "  build        build bin/gtx"
	@echo "  test         go test ./..."
	@echo "  test-race    go test -race ./..."
	@echo "  clean        remove bin/ and coverage"
	@echo "  all          mod-verify, lint, build, test"

mod-verify:
	go mod verify

tidy:
	go mod tidy

fmt:
	gofmt -w $$(find . -name '*.go' -not -path './vendor/*')
	@command -v goimports >/dev/null 2>&1 && goimports -w $$(find . -name '*.go' -not -path './vendor/*') || true

vet:
	go vet ./...

lint:
	go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_VERSION) run ./...

staticcheck:
	go run honnef.co/go/tools/cmd/staticcheck@latest ./...

build:
	mkdir -p bin
	go build -o bin/gtx ./cmd/gtx

test:
	go test ./...

test-race:
	go test -race ./...

clean:
	rm -rf bin/ coverage.out coverage.html

all: mod-verify lint build test
