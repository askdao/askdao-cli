.PHONY: build install test lint clean

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

run: build
	./$(BINARY)
