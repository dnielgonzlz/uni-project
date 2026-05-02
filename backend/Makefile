.PHONY: run migrate-up migrate-down test lint build swagger help

# Load .env if it exists (for local dev convenience)
ifneq (,$(wildcard .env))
  include .env
  export
endif

## run: start the API server
run:
	go run ./cmd/api

## migrate-up: apply all pending database migrations
migrate-up:
	go run ./cmd/migrate up

## migrate-down: roll back the most recent migration (pass STEPS=N to roll back N)
migrate-down:
	go run ./cmd/migrate down $(or $(STEPS),1)

## test: run unit tests with race detector
test:
	go test -race -short ./...

## test-integration: run integration tests (requires a running Postgres)
test-integration:
	go test -race -tags=integration ./...

## lint: run golangci-lint
lint:
	golangci-lint run ./...

## build: compile the API and migrate binaries for Linux (for AWS deployment)
build:
	GOOS=linux GOARCH=amd64 go build -o dist/api       ./cmd/api
	GOOS=linux GOARCH=amd64 go build -o dist/migrate   ./cmd/migrate

## tidy: tidy and verify Go modules
tidy:
	go mod tidy
	go mod verify

## swagger: regenerate Swagger docs from handler annotations
swagger:
	swag init -g cmd/api/main.go -o docs --parseDependency --parseInternal

## help: list available make targets
help:
	@grep -E '^## ' Makefile | sed 's/^## //'
