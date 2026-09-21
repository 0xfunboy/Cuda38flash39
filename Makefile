GO := $(if $(wildcard .tools/go/bin/go),$(CURDIR)/.tools/go/bin/go,go)
export GOPATH ?= $(CURDIR)/.tools/gopath
export GOMODCACHE ?= $(GOPATH)/pkg/mod

.PHONY: build test download-qwen engine-build engine-run
build:
	mkdir -p bin
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags='-s -w' -o bin/strixglm ./cmd/strixglm
	ln -sf strixglm bin/cuda38
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags='-s -w' -o bin/download-qwen ./tools/download_qwen.go

download-qwen:
	@mkdir -p bin
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags='-s -w' -o bin/download-qwen ./tools/download_qwen.go
	./bin/download-qwen

engine-build:
	cmake -B .engine/llama.cpp/build -S .engine/llama.cpp \
		-DGGML_CUDA=ON -DCMAKE_CUDA_ARCHITECTURES=86 \
		-DCMAKE_C_COMPILER=gcc-13 -DCMAKE_CXX_COMPILER=g++-13 -DCMAKE_CUDA_HOST_COMPILER=g++-13 \
		-DCMAKE_BUILD_TYPE=Release
	cmake --build .engine/llama.cpp/build --config Release --target llama-server llama-cli -j 16

engine-run:
	./runtime/launch-cuda.sh

test:
	$(GO) test ./...
	node --test web/tests/*.test.mjs benchmarks/*.test.mjs
