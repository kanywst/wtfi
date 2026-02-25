.PHONY: build run fmt lint test clean

BINARY_NAME=wtfi

build:
	go build -o $(BINARY_NAME) ./cmd/wtfi

run:
	go run ./cmd/wtfi

fmt:
	go fmt ./...

fmt-check:
	@if [ -n "$$(gofmt -l .)" ]; then \
		echo "Files need formatting:"; \
		gofmt -l .; \
		exit 1; \
	fi

lint:
	golangci-lint run

test:
	go test -v -race ./...

clean:
	rm -f $(BINARY_NAME)
	go clean
