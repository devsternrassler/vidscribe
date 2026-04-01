.PHONY: build test test-all vet lint clean release-dry run

BIN := vidscribe

build:
	go build -o $(BIN) .

test:
	go test ./... -count=1

test-v:
	go test ./... -count=1 -v

test-all:
	go test ./... -count=1 -timeout 120s

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
