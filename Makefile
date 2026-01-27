# Nodefy Agent Makefile

BINARY_NAME=nodefy-agent
BUILD_DIR=bin
VERSION=0.1.0

GOCMD=go
GOBUILD=$(GOCMD) build
GOCLEAN=$(GOCMD) clean
GOTEST=$(GOCMD) test
GOMOD=$(GOCMD) mod

LDFLAGS=-ldflags "-s -w -X main.version=$(VERSION)"

.PHONY: all build clean test deps run install cross help

all: clean build

## build: Build the agent binary
build:
	@echo "Building $(BINARY_NAME)..."
	@mkdir -p $(BUILD_DIR)
	$(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/agent

## run: Run the agent
run: build
	./$(BUILD_DIR)/$(BINARY_NAME) $(ARGS)

## test: Run tests
test:
	$(GOTEST) -v ./...

## clean: Clean build artifacts
clean:
	@echo "Cleaning..."
	@rm -rf $(BUILD_DIR)
	$(GOCLEAN)

## deps: Download dependencies
deps:
	$(GOMOD) tidy

## install: Install to /usr/local/bin
install: build
	@cp $(BUILD_DIR)/$(BINARY_NAME) /usr/local/bin/

## cross: Cross-compile for all platforms
cross:
	@echo "Cross-compiling..."
	@mkdir -p $(BUILD_DIR)
	GOOS=darwin GOARCH=amd64 $(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-darwin-amd64 ./cmd/agent
	GOOS=darwin GOARCH=arm64 $(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-darwin-arm64 ./cmd/agent
	GOOS=linux GOARCH=amd64 $(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-linux-amd64 ./cmd/agent
	GOOS=windows GOARCH=amd64 $(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-windows.exe ./cmd/agent
	@echo "Done!"

## help: Show this help
help:
	@echo "Nodefy Agent"
	@echo ""
	@echo "Usage: make [target]"
	@echo ""
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/## /  /'
