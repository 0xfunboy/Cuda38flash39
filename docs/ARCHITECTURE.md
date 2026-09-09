# Source layout and product boundaries

**HaloClu** is the frontend product name. **StrixHaloClusterGLM** is the
repository and reference deployment for the qualified GLM configuration. The
served model remains a separately reported backend identity. Reusing the UI
for another model does not make GLM-specific kernels, formats, performance
figures or qualification transferable to it.

| Path | Responsibility |
|---|---|
| `cmd/strixglm/main.go` | Minimal executable entry point |
| `internal/app/` | HTTP gateway, admission, paired ownership, workspace integration and their Go tests |
| `web/assets.go` | Embeds production HTML/CSS/JavaScript and selected HaloClu artwork, not browser tests |
| `web/` | HaloClu interface and dependency-free browser logic/tests |
| `runtime/assets.go` | Embeds the model catalog, not the inference engine or model weights |
| `runtime/` | Pinned engine recipe, preserved patches and Pi adapters |
| `benchmarks/` | Explicit regression/qualification scripts, separate from ordinary browsing |
| `docs/` | Product behavior and deployment documentation |
| `state/`, `bin/`, `.tools/` | Private local state and generated/downloaded artifacts; ignored by Git |

Go groups source by package directory, not one directory per type. Keeping a
cohesive application in `internal/app` is deliberate: the move does not expose
dozens of internal functions merely to create more folders. The small command
entry point and asset packages separate executable wiring from implementation
and prevent test fixtures from being embedded in the frontend.

## Build from the repository root

```sh
make build
# Equivalent build target:
go build -o bin/strixglm ./cmd/strixglm
go test ./...
```

The root is no longer an executable Go package, so use `./cmd/strixglm`, not
`go build .`. The binary path remains `bin/strixglm`; configuration filenames,
CLI commands and service identities are unchanged. Run from the repository
root or supply the appropriate configuration path. Moving source files does
not make the recorded absolute deployment paths portable.

## Runtime separation

`internal/app/conversations*.go` owns canonical transcript records and lifecycle;
the Chat stream is persisted by the gateway even after browser disconnection.
`workspace_protection.go` prepares bounded source copies and independently runs
captured build/test commands; it does not implement another coding agent.
`download*.go` uses a separate public-only HTTPS transport, durable manifests and
byte-range resume. Acquiring files neither installs nor switches the model.

The gateway serves the UI and authenticated APIs. It does not replace the
target model, the paired coordinator or the Pi agent. Frontend changes require
a gateway rebuild; a gateway restart must drain its work and must not restart
an individual inference rank. Browser Options cannot install a model, edit a
driver, move weights or silently widen the listen address.

This restructuring and the HaloClu name introduce no new model-quality or
throughput result. Retained evidence is in [qualification](../QUALIFICATION.md).
