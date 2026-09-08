# StrixHaloClusterGLM

Local coding assistant and OpenAI-compatible gateway for **two GMKtec EVO-X3**
(Ryzen AI Max+395, Radeon8060S,128GB UMA each). One GLM Flash target, TP2/PP1 over
USB4 Socket, DFlash2 k5/local0. Existing engine/math/weights are unchanged.

**READY for the qualified compact C++ coding workflow.** 10/10 distinct small
repositories, nine first-pass and one successful logical repair. Native paired
API, real browser, explicit apply, cancellation/drain, whole-pair restart and
original rollback have passed. Evidence and negatives:
[QUALIFICATION.md](QUALIFICATION.md). This is not a near-SOTA score or a guarantee
for arbitrary production repositories. Long-context coding is not qualified.

Current handoff: product UI/API **18093** uses the preserved original GLM API18091;
the old coder18092 remains available. The fully Go native coordinator18094 and
native product18095 were validated, then stopped after restoring the original
pair. Permanent adoption is an explicit maintenance choice, not an implicit
port/service switch. No automatic boot startup has been enabled.

## Measured preset

| Profile | Reasoning | Input selection / output budget | Result |
|---|---|---|---|
| FAST | low | 4096 estimated / 4096 output | Selected compact coding preset |
| BALANCED | low | Same | Explicit alias of FAST |
| QUALITY | low | Same | No higher-quality alternative demonstrated |

All three allow two repairs. On the original six-task workload: **6/6 first-pass,
20.15 seconds/correct solution, median25.65 decode TPS**. The harder incremental
UTF-8 implementation needed one genuine test-driven repair:152.45 seconds total.
These are different tasks, not repeated measurements of one speed benchmark.
On a matched two-task comparison, low took23.97s/correct, high68.81s/correct;
max delivered neither answer before8192tokens. The pinned template maps medium
to max, so there is no independently qualified medium mode.

Largest passing context-integration fixture: **2575 actual prompt tokens**.
5107–20360token fixtures failed; a30529token request ran but remained incomplete.
The4096 input budget is a byte-based selection estimate, not a qualified4K
context. Select a small relevant subproject; the50000 override is a hard
configuration ceiling, never a quality claim.

## Architecture

```text
Browser / CLI / IDE client
          |
   Go gateway :18093 (compatibility) / :18095 (native)
   context selection → coding loop → isolated compile/test → verified diff
          |
   external inference API
          ├─ compatibility: existing :18091, old coder :18092 preserved
          └─ native Go coordinator :18094 (explicit whole-pair cutover only)
                          |
                 NODE01 :18110 ⇆ NODE02 :18110
                       pinned GLM TP2
```

Go was selected for a single small binary with standard-library HTTP, streaming,
concurrency and Linux subprocess control; no application dependency modules.
CIRU/vLLM/torch remain Python where inference requires them. Native cluster
lifecycle and paired HTTP coordination are implemented in Go, not Python.

## Build and configure

Linux with Go1.27.1, Git, bubblewrap, systemd user manager and the compiler/test
tools your projects require. C++ qualification uses the existing `g++`.
Go download pin: [official Go releases](https://go.dev/dl/), archive
`go1.27.1.linux-amd64.tar.gz`, SHA256
`63d339f0da5ab53635a56f2490a7984dfe12dfcff22ad749f63edaf590168445`.
The local `.tools/go/` installation is excluded from version control.

```bash
cd /home/funboy/StrixHaloClusterGLM
make build
make test
./bin/strixglm serve --config config.json
```

`config.json`: listener, engine API, paths, workspace roots, profile budgets,
timeouts, sandbox memory/process limits. `runtime/cluster.json`: hosts, private
ports and controller paths. NODE01 USB4=`10.55.0.1`, NODE02=`10.55.0.2`, SSH alias
`02-evo-x3-tb`; configure passwordless SSH as the unprivileged user. The engine is
already installed at the external path in `runtime/manifest.json`; **do not copy
weights into this repo**. See [runtime recipe](runtime/README.md) for revisions,
rank layouts, integrity provenance and the five required correctness patches.

Service: `systemctl --user start strixglm`. Logs: `journalctl --user -u strixglm`.
The unit in `deploy/strixglm.service` is linked locally, not enabled at boot.
For a fresh user-service installation, first link the unit with
`systemctl --user link /home/funboy/StrixHaloClusterGLM/deploy/strixglm.service`
and run `systemctl --user daemon-reload`. A returned `systemctl start` is not an
HTTP readiness barrier: check `curl -f http://127.0.0.1:18093/health` before
submitting. The external whole pair must already be healthy; the gateway does
not silently recover/restart it or download assets.
Never start just one model rank. [Cluster operations](runtime/OPERATIONS.md)
explain preservation, explicit native startup and exact original rollback.

## Browser / API / CLI

Open **http://127.0.0.1:18093/** and enter the token from `state/api-token`
(mode600). Token stays in browser memory, not localStorage. For another computer,
use an SSH tunnel; no unauthenticated public listener or CORS is enabled.

```bash
./bin/strixglm status
./bin/strixglm models
./bin/strixglm chat --profile fast --message 'Explain this C++ error...'
./bin/strixglm chat --profile fast --max-tokens 512 --message 'Explain RAII briefly.'
./bin/strixglm chat                 # interactive; /clear and /quit
./bin/strixglm profile fast
./bin/strixglm code --repo /home/funboy/ai-exp/my-project \
  --task-file issue.md --allowed src/cache.cpp,include/cache.hpp \
  --build 'g++ -std=c++17 -Iinclude src/cache.cpp tests.cpp -o checks' \
  --test './checks' --profile fast
```

Allowed paths are explicit filenames or directory prefixes ending `/`. Include
normal project files with `files` in a JSON spec to override deterministic
relevance selection (issue terms, dependency references, build metadata).
Sensitive/hidden files, symlinks, known binary assets, node_modules and build
caches are excluded. Snapshots are text-only: an unrecognized non-UTF8 asset can
cause a clear setup refusal. The snapshot is size-bounded; choose a subproject
for a huge repository. Dependencies must already be available in the sandbox's
read-only system paths; arbitrary npm/Cargo environments are not qualified.
Context estimates are UTF-8 bytes/3; **actual prompt_tokens from the engine** are
authoritative measurements, not an exact Go tokenizer claim.

`./bin/strixglm code --spec task.json` supports:

```json
{
  "repo": "/home/funboy/ai-exp/my-project",
  "task": "Fix stale price cache after product replacement; preserve API.",
  "allowed_paths": ["src/cache.cpp", "include/cache.hpp"],
  "files": ["src/cache.cpp", "include/cache.hpp", "include/product.hpp"],
  "build_command": ["g++", "-std=c++17", "-Iinclude", "src/cache.cpp", "tests.cpp", "-o", "checks"],
  "test_command": ["./checks"],
  "profile": "fast", "max_repairs": 2, "timeout": 600,
  "sandbox_policy": "isolated", "apply": false
}
```

Optional `test_files` maps sandbox filenames to independent test paths: they
are mounted read-only and **not sent in the initial prompt**. Build/test command
arrays execute directly, not through an implicit shell. Network and host home
are absent; only a private writable snapshot is exposed. RAM/swap/process/CPU/
output/time limits apply. Missing dependencies are setup failures, not model
intelligence failures; no auto pip/npm/download happens in the sandbox.

The loop uses a fresh conversation with task, selected current files and concise
failure output. Final-only code, compile/test, then independent repeat in a fresh
snapshot. Incomplete reasoning is never mined for code. Same failure three times
stops; profiles may use0..6 repairs, defaults chosen from measurements. HTTP
uncertainty is not blindly retried. Cancel drains the current TP2 generation,
then stops further work: cancellation is not instantaneous GPU preemption.

Original files are unchanged by default. Result contains build/test receipts,
attempts, metrics, diff, file list and output artifacts under `state/tasks/ID/`.
Use `--apply` only when explicitly authorizing an application to the original;
browser apply requires confirmation. Changed-since-snapshot paths are refused.
Backups and an apply receipt remain beside the task. PASS means supplied tests,
not a proof of universally bug-free software.

Endpoints (Bearer token required except static UI and `/health`):

| Endpoint | Purpose |
|---|---|
| `GET /v1/models`, `POST /v1/chat/completions` | OpenAI-compatible JSON and streaming chat |
| `POST /v1/coding/tasks` | async202; same task spec as CLI |
| `GET /v1/coding/tasks/{id}` | status, attempts, metrics and verified diff |
| `POST /v1/coding/tasks/{id}/cancel` | cancel/drain, no rank-only restart |
| `POST /v1/coding/tasks/{id}/apply` | explicit `{"confirm":true}` after PASS |
| `GET /health`, `/v1/status`, `/metrics` | paired health, UI telemetry, Prometheus |

Coding API is a task/diff protocol, not OpenAI tools/Responses. Chat is text-only,
single-completion; tool/function envelopes and multimodal inputs are unsupported.
IDE clients can integrate the coding API directly; a native VSCode extension is
not claimed. Persisted results/diffs survive gateway restarts; interrupted work
is not silently replayed. For safety, automatic API application is available only
for tasks verified by the current gateway process. After a restart, review the
saved diff/receipt; an interrupted apply requires manual reconciliation, not replay.
Task timeouts are per generation, not total task time. The native coordinator's
configured600second generation deadline also applies; a larger task override
does not extend that coordinator deadline.

## Regression tests and rollback

Regression generators/runner live in [benchmarks/](benchmarks/README.md).
Golden implementation and hidden tests must be frozen before model submission.
Large raw research output belongs outside the repository. Measured TTFT client
and server are distinguished; decodeTPS=`(completion_tokens−1)/generation_time`,
HTTP includes prefill and transport. Unavailable telemetry remains null.

`systemctl --user stop strixglm` disables only the new gateway/UI. Original
`http://127.0.0.1:18091/v1` and coder18092 remain available in compatibility mode.
Do not delete the fallback. Native whole-pair validation requires an explicit
snapshot/stop/start/restart/restore window; use the ownership-aware commands, not
`pkill` or isolated `systemctl restart` on a rank. Nothing touches8080/50052 or FULL.
