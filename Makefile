.PHONY: go build dist openapi generate ytapi contract

YTRACK_DEV_STATE ?= $(CURDIR)/dev/.state
# Календарная версия релиза, v<ГГ>.<М>.<Д>.<номер запуска CI>. Пустая оставляет версию, которую ставит go build.
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

openapi:
	scripts/openapi.sh

generate:
	go generate ./...

ytapi:
	scripts/ytapi.sh

contract:
	YTRACK_DEV_STATE=$(YTRACK_DEV_STATE) go test -tags contract -count=1 ./internal/cli/
