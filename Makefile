.PHONY: go build dist

VERSION ?=

go:
	test -z "$$(gofmt -l cmd internal)" || { gofmt -l cmd internal; exit 1; }
	go vet ./...
	go test -race ./...

build:
	go build -ldflags '-X main.version=$(VERSION)' -o bin/ytrack ./cmd/ytrack

dist:
	scripts/dist.sh '$(VERSION)'
