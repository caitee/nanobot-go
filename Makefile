.PHONY: build test lint fmt fmt-check check clean install

build:
	go build -o nanobot ./cmd/nanobot
	go build -o gateway ./cmd/gateway

test:
	go test -race ./...

lint:
	go vet ./...

fmt:
	go fmt ./...

fmt-check:
	@files="$$(find . -name '*.go' -not -path './vendor/*' -exec gofmt -l {} +)"; \
	if [ -n "$$files" ]; then \
		printf "Unformatted Go files:\n%s\n" "$$files"; \
		exit 1; \
	fi

check: fmt-check lint test

clean:
	rm -f nanobot gateway

install:
	go install ./cmd/nanobot
	go install ./cmd/gateway
