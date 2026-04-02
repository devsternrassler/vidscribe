.PHONY: build test test-v test-smoke test-e2e test-bench test-all vet lint clean release-dry run

BIN := vidscribe

build:
	go build -o $(BIN) .

# Unit tests only — no external deps, <1s
test:
	go test ./... -count=1

test-v:
	go test ./... -count=1 -v

# Smoke tests — requires uvx + ffmpeg in PATH (includes unit tests)
test-smoke:
	go test -tags=smoke ./... -count=1 -v

# E2E tests — requires uvx + ffmpeg + network (includes unit + smoke)
# Override video: VIDSCRIBE_TEST_URL=https://... make test-e2e
# Override browser: VIDSCRIBE_TEST_BROWSER=firefox make test-e2e
test-e2e:
	go test -tags=e2e ./... -count=1 -v -timeout 600s

# Performance comparison table + Go benchmarks (requires uvx + ffmpeg + network)
test-bench:
	go test -tags=e2e ./internal/pipeline/ -run TestE2E_PerformanceComparison -count=1 -v -timeout 600s
	go test -tags=e2e ./internal/pipeline/ -bench=BenchmarkTranscribe -benchmem -count=1 -timeout 600s

# All tests with extended timeout
test-all:
	go test -tags=e2e ./... -count=1 -v -timeout 600s

vet:
	go vet ./...

lint: vet
	go mod tidy
	git diff --exit-code go.mod go.sum

clean:
	rm -f $(BIN)
	rm -rf dist/

release-dry:
	goreleaser release --snapshot --clean

run:
	go run . $(ARGS)
