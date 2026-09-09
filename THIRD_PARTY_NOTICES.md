# Third-party notices

This repository contains original gateway, controller and browser integration
code, small modifications to an external inference runtime, and an adapter
derived from an upstream Pi example. A third-party license below applies to
its identified component; it does not assign that license to unrelated
original project code. The repository owner's original-code license is a
separate decision.

## Pi SSH example adaptation

`runtime/pi-ssh.ts` adapts the upstream Pi SSH extension example. Pi remains
the external coding agent; it is installed separately and is not vendored here.

- Upstream project: [earendil-works/pi](https://github.com/earendil-works/pi).
- Pinned revision: `d981de1229ef899957bbe968bc8dcda02a21f477`.
- Original example: [packages/coding-agent/examples/extensions/ssh.ts](https://github.com/earendil-works/pi/blob/d981de1229ef899957bbe968bc8dcda02a21f477/packages/coding-agent/examples/extensions/ssh.ts).
- Copyright (c) 2025 Mario Zechner.
- License: MIT; the complete notice is in [docs/licenses/PI-MIT.txt](docs/licenses/PI-MIT.txt), copied from the [pinned upstream LICENSE](https://github.com/earendil-works/pi/blob/d981de1229ef899957bbe968bc8dcda02a21f477/LICENSE).

Local changes provide explicitly configured SSH transport, quoting and path
checks, bounded output and timeouts, cancellation handling, password-broker
integration through OpenSSH, and text-only file tools. These changes do not
make a remote SSH account an operating-system sandbox.

The separately installed package is `@earendil-works/pi-coding-agent@0.85.1`.
Its source and artifact identity are recorded in [runtime/pi-pin.json](runtime/pi-pin.json).
The package and its transitive dependencies retain their respective licenses
in that installation; they are not included in this Git repository.

## CIRU/vLLM correctness patches

The five files under `runtime/patches/` are local changes to the external
CIRU-distributed vLLM runtime, not a copy of the complete runtime. Their purpose
is safe prefill, canonical MoE execution, stable routing ties, coherent tile
selection, and the qualified F1 WMMA path. Patch hunks preserve available
upstream SPDX/copyright context.

The patched vLLM sources identify `Apache-2.0` and
`Copyright contributors to the vLLM project`. The complete Apache License 2.0
text is in [docs/licenses/VLLM-APACHE-2.0.txt](docs/licenses/VLLM-APACHE-2.0.txt).
It was copied from the installed qualified distribution's
`vllm-0.1.0rc2.dev9+g9255fd9fb9.rocm100.dist-info/licenses/LICENSE`.
No separate NOTICE file was found in that distribution's license directory.

The exact CIRU source/wheel identities and local modifications are recorded in
[runtime/manifest.json](runtime/manifest.json). Rebuild instructions and patch
order are in [runtime/README.md](runtime/README.md). Model and runtime provenance:
[jcbtc/GLM5.3-Flash-CIRU-STRIX-IU4](https://huggingface.co/jcbtc/GLM5.3-Flash-CIRU-STRIX-IU4/tree/a1ab828d8bd5b0ba8e3281febfe3d34b12072040).

## External engines, models and tools

No model weights, drafter weights, compiled kernels, framework wheels, Go/Node
toolchains, Pi dependency tree, or downloaded archives are distributed here.
The external GLM model, DFlash2 checkpoint, CIRU components, vLLM, torch, ROCm,
OpenSSH, bubblewrap and other system tools retain their own applicable terms.
Source links and immutable artifact metadata are not an ownership claim or
permission to relicense those components. Names are used to identify the
tested integration; no upstream endorsement is claimed.
