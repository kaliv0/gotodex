.PHONY: help fmt vet test build tidy clean all ci

help:
	@echo "Targets:"
	@echo "  tidy    go mod tidy"
	@echo "  fmt     gofmt + goimports if present"
	@echo "  vet     go vet"
	@echo "  test    go test ./..."
	@echo "  build   build bin/gtx"
	@echo "  clean   remove bin/ and coverage"
	@echo "  ci      fmt check + vet + test"
	@echo "  all     tidy fmt vet test build"

tidy:
	go mod tidy

fmt:
	gofmt -w $$(find . -name '*.go' -not -path './vendor/*')
	@command -v goimports >/dev/null 2>&1 && goimports -w $$(find . -name '*.go' -not -path './vendor/*') || true

vet:
	go vet ./...

test:
	go test ./...

build:
	mkdir -p bin
	go build -o bin/gtx ./cmd/gtx

clean:
	rm -rf bin/ coverage.out coverage.html

ci:
	@test -z "$$(gofmt -l $$(find . -name '*.go' -not -path './vendor/*'))" || (echo "gofmt needed:" && gofmt -l $$(find . -name '*.go' -not -path './vendor/*') && exit 1)
	go vet ./...
	go test ./...

all: tidy fmt vet test build
