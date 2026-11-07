# Every recipe is a single go or gofmt command, so this works the same on Windows
# (cmd.exe or PowerShell), macOS and Linux. Anything more involved lives in tools/check.

# Variables
COVERAGE_THRESHOLD := 85.0

# Tools run with go run at pinned versions, so nothing needs installing and everyone
# gets the same results. Bump these deliberately.
GOLANGCI_LINT := github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.15.0
GOVULNCHECK   := golang.org/x/vuln/cmd/govulncheck@v1.9.0


# Run everything CI should before merging
check: fmt-check vet lint cover vuln


# Format all Go files
fmt:
	@echo Formatting...
	@gofmt -w .


# Fail if any Go file isn't formatted. Windows line endings don't count
fmt-check:
	@go -C tools run ./check -root .. fmt


# Report suspicious code
vet:
	@echo Running go vet...
	@go vet ./...


# Lint using golangci-lint
lint:
	@echo Running golangci-lint...
	@go run $(GOLANGCI_LINT) run ./...


fix-lint:
	@echo Auto-fixing lint issues...
	@go run $(GOLANGCI_LINT) run --fix ./...


# Report known vulnerabilities in the dependencies and standard library that the code calls
vuln:
	@echo Running govulncheck...
	@go run $(GOVULNCHECK) ./...


# Run all tests
test:
	@go test ./... -v


# Run the tests CI runs with coverage, and fail if below threshold
cover:
	@go -C tools run ./check -root .. cover -threshold $(COVERAGE_THRESHOLD)


# Run tests with the race detector. Needs cgo, so a C compiler such as gcc
race:
	@go test -race -count=1 ./...


# Run the benchmarks and save the output to bench/results
bench:
	@go -C bench run ./cmd/record


# Install dependencies
deps:
	@go mod tidy

.PHONY: check fmt fmt-check vet lint fix-lint vuln test cover race bench deps
