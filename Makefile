.PHONY: build test vet fmt lint verify tidy fixture

build:
	go build ./...

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -w .

lint:
	golangci-lint run

# fixture builds the hermetic fixture module (a separate module under testdata).
fixture:
	cd testdata/fixtures/loansvc && go build ./...

# verify is the per-phase gate: it must stay green at the end of every phase.
verify: build vet test fixture
	@out=$$(gofmt -l . | grep -v '^testdata/fixtures/loansvc/' || true); \
	if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi
	@echo "verify OK"

tidy:
	go mod tidy
