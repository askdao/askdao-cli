.PHONY: build install test lint clean snapshot

BINARY := askdao
PKG    := github.com/askdao/askdao-cli/cmd/askdao

build:
	go build -o $(BINARY) $(PKG)

install:
	go install $(PKG)

test:
	go test ./...

lint:
	go vet ./...
	gofmt -l . | tee /dev/stderr | (! read)

clean:
	rm -f $(BINARY)
	rm -rf dist/

# Local dry-run of the release pipeline (needs goreleaser installed).
snapshot:
	goreleaser release --snapshot --clean

run: build
	./$(BINARY)
