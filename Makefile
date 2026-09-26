.PHONY: go build dist generate ytapi

VERSION ?=

go:
	test -z "$$(gofmt -l cmd internal scripts)" || { gofmt -l cmd internal scripts; exit 1; }
	go vet ./...
	go vet scripts/ytapi-imports.go
	go vet scripts/catalogue.go
	go test -race ./...

build:
	go build -ldflags '-X main.version=$(VERSION)' -o bin/ytrack ./cmd/ytrack

dist:
	scripts/dist.sh '$(VERSION)'

generate:
	go generate ./...

ytapi:
	scripts/ytapi.sh
