.PHONY: build test test-unit test-race vet fmt lint tidy clean

# Build all packages.
build:
	go build ./...

# Run the full test suite (offline; no credentials or network required).
test:
	go test ./...

# Run the tests with a verbose reporter.
test-unit:
	go test ./... -v

# Run the tests under the race detector with coverage.
test-race:
	go test -race -cover ./...

# Vet the code.
vet:
	go vet ./...

# Check formatting.
fmt:
	@test -z "$$(gofmt -l .)" || (echo "Run 'make fmt-fix' to format:" && gofmt -l . && exit 1)

# Apply formatting.
fmt-fix:
	gofmt -w .

# Tidy the module dependencies.
tidy:
	go mod tidy

# Remove build artefacts.
clean:
	go clean -cache -testcache
