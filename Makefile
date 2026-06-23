.PHONY: build install run clean test

BINARY := session-search
BIN_DIR := bin

build:
	mkdir -p $(BIN_DIR)
	go build -ldflags="-s -w" -o $(BIN_DIR)/$(BINARY) ./cmd/session-search

install: build
	mkdir -p $(HOME)/.local/bin
	install -m 755 $(BIN_DIR)/$(BINARY) $(HOME)/.local/bin/$(BINARY)

run: build
	./$(BIN_DIR)/$(BINARY)

clean:
	rm -rf $(BIN_DIR)

# Quick development loop
dev:
	go run ./cmd/session-search

# Note: add real tests in future. `make test` must fail on real test or build failures.
test:
	go test ./...

# Example: search and get JSON for another tool
example-json:
	go run ./cmd/session-search --query "skill" --json | head -c 800
