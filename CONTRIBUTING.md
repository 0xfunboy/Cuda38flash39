# Contributing

Keep changes small, explain the failure they address and add a reproducer.
Preserve the existing qualified engine and whole-pair ownership. Do not add a
second agent loop: Pi owns coding; this repository supplies the UI, transport,
controls and verification boundaries.

## Source layout

- `cmd/strixglm/`: the executable entry point; `make build` still writes
  `bin/strixglm` and does not change the installed service names.
- `internal/app/`: gateway, CLI, cluster controls, Pi transport and their tests.
  These remain one cohesive package so behavior-preserving moves do not require
  exporting private implementation details merely to split files.
- `web/`: browser sources and the explicit four-file production asset embed.
- `runtime/`: pinned runtime recipes, adapters and the embedded model catalog;
  model weights and runtime binaries are not compiled into the gateway.
- `benchmarks/`: opt-in harnesses and fixtures; `docs/`: product documentation.

Run build and test commands from the repository root. Direct builds use
`go build -o bin/strixglm ./cmd/strixglm`; `go build .` is no longer an executable
target. Runtime configuration and private state paths are unchanged.

## Checks

Hosted [portable checks](.github/workflows/checks.yml) compile Go, run static
checks, eight selected protocol fixtures and the Node mock/unit suite. They do
not run the full hardware-sensitive tests, install Pi, contact a model, connect
SSH, download weights or test GPU performance. Actions are pinned to exact
upstream commits and receive read-only repository permissions.

On the configured reference machine, the complete CPU/local-integration suite is:

```sh
make test
.tools/go/bin/go test -race ./...
.tools/go/bin/go vet ./...
```

The full local suite requires Linux, the pinned toolchain, compilers, bubblewrap,
user-systemd scopes, PDF utilities and the installed Pi package. Report missing
prerequisites or skipped checks explicitly. Real inference/browser action
scripts are **opt-in**, not part of hosted CI; do not run them against someone
else's active session or overwrite a completed report.

## Evidence and review

- Distinguish fixture, mock, live integration, fidelity and model-quality results.
- Keep negative outcomes, exact workload/settings, scope and completion status.
- A token cap is INCOMPLETE; a test count is not an intelligence score.
- Do not weaken tolerances, allow approximate target paths or guess tool IDs to
  turn a failure into a pass. No independent rank restart.
- Keep new UI and documentation in English; add optional Italian labels where
  applicable. Use minimal grayscale components, safe rendering and no CDN.
- Do not commit generated artifacts, credentials, models or private transcripts.

Read [third-party notices](THIRD_PARTY_NOTICES.md) before copying upstream code.
Original-code licensing is the owner's explicit choice; do not assume a
different license for third-party patches or model weights.
