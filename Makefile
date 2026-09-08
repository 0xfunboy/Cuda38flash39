GO := $(if $(wildcard .tools/go/bin/go),$(CURDIR)/.tools/go/bin/go,go)

.PHONY: build test
build:
	mkdir -p bin
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags='-s -w' -o bin/strixglm .
test:
	$(GO) test ./...
	node --test web/tests/*.test.mjs benchmarks/*.test.mjs
