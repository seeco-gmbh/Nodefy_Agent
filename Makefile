.PHONY: build clean test

BINARY_NAME=nodefy-agent
BUILD_DIR=build

# Build for current platform (with systray)
build:
	@mkdir -p $(BUILD_DIR)
	go build -ldflags="-s -w" -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/agent
	@echo "Built: $(BUILD_DIR)/$(BINARY_NAME)"

# Clean build artifacts
clean:
	rm -rf $(BUILD_DIR)

# Run tests
test:
	go test -v ./...

# Run locally
run:
	go run ./cmd/agent --local --debug
