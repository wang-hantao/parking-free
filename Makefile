.PHONY: help build run ingest test test-cover lint fmt tidy migrate docker-up docker-down clean

# Default
help:
	@echo "Targets:"
	@echo "  build       - Compile both binaries to ./bin/"
	@echo "  run         - Run the HTTP server"
	@echo "  ingest      - Run the LTF-Tolken ingester"
	@echo "  test        - Run all tests"
	@echo "  test-cover  - Run tests with coverage"
	@echo "  lint        - Run golangci-lint"
	@echo "  fmt         - Format code"
	@echo "  tidy        - go mod tidy"
	@echo "  migrate     - Apply Postgres migrations"
	@echo "  docker-up   - Start local Postgres+PostGIS"
	@echo "  docker-down - Stop local Postgres+PostGIS"

build:
	@mkdir -p bin
	go build -o bin/server ./cmd/server
	go build -o bin/ingester ./cmd/ingester

run:
	go run ./cmd/server

ingest:
	go run ./cmd/ingester

test:
	go test ./...

test-cover:
	go test -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html

lint:
	golangci-lint run

fmt:
	gofmt -s -w .
	goimports -w .

tidy:
	go mod tidy

migrate:
	@for f in migrations/*.sql; do \
		echo ">> applying $$f"; \
		docker compose exec -T postgres psql -U parking -d parking -f - < $$f; \
	done

docker-up:
	docker compose up -d

docker-down:
	docker compose down

clean:
	rm -rf bin/ coverage.out coverage.html
