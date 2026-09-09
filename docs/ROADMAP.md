# Product roadmap

HaloClu is the model-independent product name; this repository preserves the
GLM reference runtime and upstream Pi. The immediate goal is a dependable,
lightweight interface around that deployment. The items below are proposals, not delivered
features or authorization to run additional model campaigns. Existing behavior
and evidence remain in [qualification](../QUALIFICATION.md) and
[workspaces](../WORKSPACES.md).

## Priorities

PRODUCT-007 delivers local persistent Chat/Pi history, explicit handoff/import/
deletion, protected local Pi copies with independent command receipts, and native
Go public-source downloads. These are product features, not new model-quality
claims. Portable CI now also covers their CPU-only protocol and safety fixtures;
full bootstrap and real remote-host qualification remain separate work.

| Priority | Improvement | Completion criterion |
|---|---|---|
| 1 | Reproducible bootstrap on another supported host | Pinned dependency installation, preflight for required tools and user scopes, actionable missing-dependency errors, and no implicit driver or model installation |
| 1 | Broaden portable CI coverage | Initial hosted compilation/protocol/Node checks are included; extend CPU-only coverage while keeping Pi/systemd/hardware qualification explicit and opt-in |
| 2 | Broader tool-protocol regression fixtures | Missing/duplicate arguments, numeric boundaries, malformed or incomplete calls, multiple calls and tool-result association fail safely without executing invalid tools |
| 2 | Restricted SSH workflow qualification | One real restricted host passes login/read/edit/test/cleanup; key and password paths are identified separately instead of claiming both from mocks |
| 2 | Clearer remote approvals and interruption state | Users can see authority and affected root before work; partial changes, disconnection and detached-process limitations are explicit; no silent command replay |
| 3 | Small real-repository evaluation set | Bug fix, feature and refactor are scored by independent checks, including failed attempts and time to correct delivery, without turning assertion counts into task counts |

## Design constraints

- Keep upstream Pi as the coding agent. Improve the adapter and workspace UI,
  not a parallel home-grown agent loop or a full browser IDE.
- Preserve the qualified model, paired ownership, strict result comparison and
  whole-pair rollback. UI work must not silently restart one inference rank.
- Keep secrets, raw conversations, uploads, model weights and host-specific
  runtime state out of the public repository. Sample configuration is not a
  substitute for deployment credentials or an automated hardware installer.
- Label mocked, CPU-only, live-protocol and model-quality tests separately.
  Do not promote a UI regression count into a model intelligence score.
- Keep performance claims tied to recorded workloads. Treat a small repeatable
  improvement as small; do not hide correctness losses behind tokens/s.
- Keep English as the product/documentation default with optional Italian UI
  localization. Avoid claims that a reasoning label guarantees quality.
- Keep common interface and API controls in Options, apart from generation
  settings. A shared frontend name does not qualify a different model backend.

## Deliberately out of scope

No new runtime, model download, quantization, driver, networking stack or
distributed-inference architecture is required for these product improvements.
Multimodal support, unrestricted remote automation and broad model benchmarking
are separate decisions, not prerequisites for supervised daily use.
