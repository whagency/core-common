# Makefile for Common Module

GO_RUN := GOTOOLCHAIN=go1.25.8+auto go run
GOVULNCHECK := $(GO_RUN) golang.org/x/vuln/cmd/govulncheck@v1.1.4
GOLANGCI_LINT := $(GO_RUN) github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.11.4

# Check for vulnerable dependencies using govulncheck.
.PHONY: vulnerable-check
vulnerable-check:
	$(GOVULNCHECK) ./...

# Linting using golangci-lint.
.PHONY: lint
lint:
	$(GOLANGCI_LINT) run --config .golangci.yml

.PHONY: gen-proto
gen-proto:
	@find grpc/proto -name "*.proto" -print0 | xargs -0 protoc \
		--proto_path=grpc/proto \
		--go_out=. --go_opt=paths=import \
		--go-grpc_out=. --go-grpc_opt=paths=import

.PHONY: clean-pb
clean-pb:
	rm -rf grpc/pb/*

.PHONY: generate
generate: gen-proto
