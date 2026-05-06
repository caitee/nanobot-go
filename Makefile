.PHONY: build test lint clean install

build:
	go build -o nanobot ./cmd/nanobot
	go build -o gateway ./cmd/gateway

test:
	go test -race ./...

lint:
	go vet ./...

clean:
	rm -f nanobot gateway

install:
	go install ./cmd/nanobot
	go install ./cmd/gateway
