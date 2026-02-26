.PHONY: build run fmt lint test clean

build:
	mkdir -p bin
	cd cmd/wtfi && go build -o ../../bin/wtfi

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
	rm -rf bin/
	rm -f wtfi
	go clean
