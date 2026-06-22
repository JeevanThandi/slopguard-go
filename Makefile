BINARY := slopguard-go
PKG := ./cmd/slopguard-go

.PHONY: build test vet fmt fmt-check dogfood install clean

build: ## Build the CLI binary
	go build -o $(BINARY) $(PKG)

test: ## Run the test suite with race detector + coverage
	go test -race -coverprofile=coverage.txt ./...

vet: ## Run go vet
	go vet ./...

fmt: ## Format all packages
	gofmt -w core coverage cli cmd sampleapps

fmt-check: ## Fail if any file is unformatted
	@unformatted=$$(gofmt -l core coverage cli cmd sampleapps); \
	if [ -n "$$unformatted" ]; then echo "Needs gofmt:"; echo "$$unformatted"; exit 1; fi

dogfood: build ## Run slopguard-go on its own source (complexity-only)
	./$(BINARY) analyze --path ./core --no-coverage --fail-over 300

install: ## Install the CLI to $GOBIN / $GOPATH/bin
	go install $(PKG)

clean: ## Remove build artifacts
	rm -f $(BINARY) coverage.txt
