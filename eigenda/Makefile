.PHONY: build test lint docker-build docker-up docker-down

build:
	go build -o bin/prober ./cmd/prober

test:
	go test ./...

lint:
	golangci-lint run ./...

docker-build:
	docker build -t bonda-eigenda-prober .

docker-up:
	docker compose up -d

docker-down:
	docker compose down
