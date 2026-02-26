.PHONY: build run fmt lint test clean

build:
	go build -o bin/ github.com/kanywst/wtfi/cmd/wtfi

run:
	go run github.com/kanywst/wtfi/cmd/wtfi

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
	rm -rf bin/
	go clean
