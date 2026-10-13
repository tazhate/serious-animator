BIN := dist/serious-animator

.PHONY: test build example clean

test:
	go vet ./...
	go test ./...

build:
	CGO_ENABLED=0 go build -trimpath -o $(BIN) ./cmd/serious-animator

example: build
	$(BIN) validate examples/dns
	$(BIN) render examples/dns --no-audio --quality low -o out/dns-silent.mp4

clean:
	rm -rf dist out examples/*/preview
