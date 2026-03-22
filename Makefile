.PHONY: build test clean install

build:
	go build -o nanobot ./cmd/nanobot
	go build -o gateway ./cmd/gateway

test:
	go test ./...

clean:
	rm -f nanobot gateway

install:
	go install ./cmd/nanobot
	go install ./cmd/gateway
