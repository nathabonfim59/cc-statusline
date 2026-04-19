BINARY  := claude-statusline
REPO    := nathabonfim59/claude-statusline
BIN_DIR := bin
TAG     ?= $(VERSION)
VERSION ?= $(shell git describe --tags --always --dirty)
LDFLAGS := -s -w

.PHONY: all build release clean

all: build

build:
	mkdir -p $(BIN_DIR)
	GOOS=linux   GOARCH=amd64  CGO_ENABLED=0 go build -ldflags="$(LDFLAGS)" -o $(BIN_DIR)/$(BINARY)-linux-amd64 .
	GOOS=linux   GOARCH=arm64  CGO_ENABLED=0 go build -ldflags="$(LDFLAGS)" -o $(BIN_DIR)/$(BINARY)-linux-arm64 .
	GOOS=linux   GOARCH=amd64  CGO_ENABLED=0 go build -ldflags="$(LDFLAGS)" -o $(BIN_DIR)/$(BINARY)-linux-amd64-musl .
	GOOS=darwin  GOARCH=amd64  CGO_ENABLED=0 go build -ldflags="$(LDFLAGS)" -o $(BIN_DIR)/$(BINARY)-darwin-amd64 .
	GOOS=darwin  GOARCH=arm64  CGO_ENABLED=0 go build -ldflags="$(LDFLAGS)" -o $(BIN_DIR)/$(BINARY)-darwin-arm64 .
	GOOS=windows GOARCH=amd64  CGO_ENABLED=0 go build -ldflags="$(LDFLAGS)" -o $(BIN_DIR)/$(BINARY)-windows-amd64.exe .

release: build
	@if [ -z "$(TAG)" ]; then echo "Usage: make release TAG=v1.0.0"; exit 1; fi
	git tag $(TAG)
	@echo ""
	@echo "Tag $(TAG) created. Push it with:"
	@echo ""
	@echo "  git push origin $(TAG)"
	@echo ""
	@echo "Then attach the binaries from $(BIN_DIR)/ to the GitHub release."

clean:
	rm -rf $(BIN_DIR)
