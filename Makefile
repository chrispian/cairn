.DEFAULT_GOAL := check

.PHONY: check fmt vet lint test vulncheck build clean

check: fmt vet lint test vulncheck ## Run everything CI runs

fmt: ## Verify formatting (no output = clean)
	@out=$$(gofmt -l .); \
	if [ -n "$$out" ]; then echo "Files need gofmt:"; echo "$$out"; exit 1; fi

vet:
	go vet ./...

lint:
	golangci-lint run --max-same-issues=0 --max-issues-per-linter=0

test:
	go test -race -count=1 ./...

vulncheck:
	govulncheck ./...

build:
	go build -o bin/cairn ./cmd/cairn

clean:
	rm -rf bin
