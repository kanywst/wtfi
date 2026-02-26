.PHONY: build run fmt lint test clean

build:
	mkdir -p bin
	go build -o bin/wtfi ./cmd/wtfi/main.go

run:
	go run ./cmd/wtfi/main.go

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
	rm -f wtfi
	go clean
