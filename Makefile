GO := $(if $(wildcard .tools/go/bin/go),$(CURDIR)/.tools/go/bin/go,go)
export GOPATH ?= $(CURDIR)/.tools/gopath
export GOMODCACHE ?= $(GOPATH)/pkg/mod

.PHONY: build test
build:
	mkdir -p bin
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags='-s -w' -o bin/strixglm ./cmd/strixglm
test:
	$(GO) test ./...
	node --test web/tests/*.test.mjs benchmarks/*.test.mjs
