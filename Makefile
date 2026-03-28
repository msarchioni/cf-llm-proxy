BINARY_NAME=cf-llm-proxy
VERSION=1.0.0
BUILD_DIR=build

.PHONY: all build clean darwin-arm64 darwin-amd64 linux-amd64 windows-amd64

all: darwin-arm64 linux-amd64 windows-amd64

build:
	go build -ldflags "-s -w -X main.version=$(VERSION)" -o $(BINARY_NAME) ./cmd/proxy

darwin-arm64:
	GOOS=darwin GOARCH=arm64 go build -ldflags "-s -w -X main.version=$(VERSION)" -o $(BUILD_DIR)/$(BINARY_NAME)-darwin-arm64 ./cmd/proxy

darwin-amd64:
	GOOS=darwin GOARCH=amd64 go build -ldflags "-s -w -X main.version=$(VERSION)" -o $(BUILD_DIR)/$(BINARY_NAME)-darwin-amd64 ./cmd/proxy

linux-amd64:
	GOOS=linux GOARCH=amd64 go build -ldflags "-s -w -X main.version=$(VERSION)" -o $(BUILD_DIR)/$(BINARY_NAME)-linux-amd64 ./cmd/proxy

windows-amd64:
	GOOS=windows GOARCH=amd64 go build -ldflags "-s -w -X main.version=$(VERSION)" -o $(BUILD_DIR)/$(BINARY_NAME)-windows-amd64.exe ./cmd/proxy

clean:
	rm -rf $(BUILD_DIR) $(BINARY_NAME)

install: build
	cp $(BINARY_NAME) /usr/local/bin/
