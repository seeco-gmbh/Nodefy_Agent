.PHONY: build build-app clean test run

BINARY_NAME=nodefy-agent
BIN_DIR=bin
BUILD_DIR=build
APP_NAME=Nodefy Agent.app

# Build binary for current platform (outputs to bin/ for dev use)
build:
	@mkdir -p $(BIN_DIR)
	CGO_ENABLED=1 go build -ldflags="-s -w" -o $(BIN_DIR)/$(BINARY_NAME) ./cmd/agent
	@echo "Built: $(BIN_DIR)/$(BINARY_NAME)"

# Build macOS .app bundle (for distribution)
build-app: build
	@echo "Creating macOS app bundle..."
	@mkdir -p "$(BUILD_DIR)/$(APP_NAME)/Contents/MacOS"
	@mkdir -p "$(BUILD_DIR)/$(APP_NAME)/Contents/Resources"
	@cp $(BIN_DIR)/$(BINARY_NAME) "$(BUILD_DIR)/$(APP_NAME)/Contents/MacOS/Nodefy Agent"
	@printf '%s\n' \
		'<?xml version="1.0" encoding="UTF-8"?>' \
		'<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">' \
		'<plist version="1.0">' \
		'<dict>' \
		'    <key>CFBundleExecutable</key>' \
		'    <string>Nodefy Agent</string>' \
		'    <key>CFBundleIdentifier</key>' \
		'    <string>com.nodefy.agent</string>' \
		'    <key>CFBundleName</key>' \
		'    <string>Nodefy Agent</string>' \
		'    <key>CFBundleVersion</key>' \
		'    <string>0.2.0</string>' \
		'    <key>CFBundlePackageType</key>' \
		'    <string>APPL</string>' \
		'    <key>LSUIElement</key>' \
		'    <true/>' \
		'    <key>LSMinimumSystemVersion</key>' \
		'    <string>11.0</string>' \
		'</dict>' \
		'</plist>' > "$(BUILD_DIR)/$(APP_NAME)/Contents/Info.plist"
	@codesign --force --deep --sign - "$(BUILD_DIR)/$(APP_NAME)" 2>/dev/null || true
	@echo "Built: $(BUILD_DIR)/$(APP_NAME)"

# Clean build artifacts
clean:
	rm -rf $(BIN_DIR) $(BUILD_DIR)

# Run tests
test:
	go test -v ./...

# Run locally with debug
run:
	go run ./cmd/agent --debug
