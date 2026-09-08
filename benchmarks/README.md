# Offline regression suite

Prepare the deterministic C++17 fixtures and independent tests before any model
request:

```sh
node benchmarks/generate.mjs /absolute/new/report-directory
```

Requires Node, g++, diff, prlimit and unprivileged bubblewrap. No network or model
is used. All compiler and test processes run in a network-isolated namespace
with no host home mounted. Each golden must compile and pass; its deliberately
buggy variant must compile and fail. A failed setup is **not** a model failure.

The six practical regression tasks cover escaping, expiring LRU state,
scheduling interval subtraction, transactional input validation, a three-file
catalog/cache refactor, and queue/worker error recovery. The manifest freezes
source, hidden-test and golden-patch hashes. Generation refuses to overwrite an
existing frozen manifest. The hidden oracle and golden implementation live in
`private/`, outside the buggy repository; only explicit `files` are prompt input.
Do not index the generator source or private directory into a coding request.

An additive seventh task exercises a binary incremental decoder across four
source files: split headers/payloads/checksums, unsigned bytes, frame limits,
transactional errors, reset and retry after corruption. Two injected buffering
regressions interact. Its independent encoder tests every split point and
multiple chunk strides; no test requires deliberate model failure.

An eighth task is a small tenant-settings service with inheritance, tombstones,
dependency-aware positive/negative caching and atomic reparenting. Its two
regressions are distributed across resolver and mutation implementations.

To prepare only that task and a larger 95-policy context fixture in a new report:

```sh
node benchmarks/generate.mjs /absolute/new/extra-report \
  --only 08-tenant-settings-cache,context-32k-expanded --context-count 95
```

Six context sanity fixtures grow a service registry with distinct exported
factories and contracts. Every module must be wired into the result, and its
behavior is independently tested. Nominal 1K..32K labels are approximate; report
the runtime's actual prompt tokens. This is cross-file integration coverage,
**not** evidence of arbitrary software-engineering reliability at 32K tokens.

Each `task.json` is a coding API request template: set the desired measured
profile, then submit through the new product. Tests are injected separately via
`test_files`; they are never included in the initial model prompt. Treat test
receipts, candidate patches and model timings as artifacts outside this repo.

Run a selected comparison through the actual product (this **does** generate
model requests; prepare/freeze first):

```sh
node benchmarks/run.mjs --suite /absolute/report/manifest.json \
  --profiles low,medium,max --ids 01-escaped-settings,05-price-cache-refactor \
  --output /absolute/new/results --max-tokens 4096 --max-repairs 2
```

Default endpoint is loopback port 18093; `--token-file` selects the product API
token. `--dry-run` sends no requests. Tasks are sequential and profiles are
interleaved per fixture. GET status polling does not call GLM. Re-running the
same command resumes recorded task IDs and skips terminal tasks. An interrupted
submission without an ID is ambiguous and stops fail-closed rather than
resubmitting. Changed input requires a new output directory.

`summary.json` reports successful solutions per hour and total task time per
correct solution **including failed task time**, not just TPS of successes.
Missing telemetry stays null. Requested and effective reasoning are recorded;
the currently pinned template maps unsupported `medium` to `max`, so do not
claim an independent medium mode without an engine change.

`node --test benchmarks/run.test.mjs` exercises aggregation and resume against a
local mock API, never GLM. Node is an optional benchmark preparation/analysis
tool, not a dependency of the running product.

No fixture solution may be patched manually after a model run and then counted
as a model success. PASS requires the delivered patch to pass the frozen oracle
and preserve the repository/API contract; it is not a universal quality claim.

## One-request JSON API smoke

This explicitly authorized smoke sends one nonstreaming chat request, with
reasoning low, temperature 0, seed 1 and output cap 64. It verifies the pinned GLM,
the answer `42`, natural completion, token usage, engine metrics, and an idle
healthy pair before and after. It does not run coding tasks or restart anything.

```sh
node benchmarks/api-smoke.mjs --endpoint http://127.0.0.1:18093 \
  --token-file state/api-token --output /absolute/new/json-api-smoke --run-live

node benchmarks/api-smoke.mjs --endpoint http://127.0.0.1:18095 \
  --token-file state/native-gateway/api-token --output /absolute/new/native-json-api-smoke --run-live
```

The output parent directory must already exist. Existing output is refused;
intent is written before the POST, and an ambiguous response is never retried.
Raw request/response, server TTFT and full HTTP time are retained separately.
Client TTFT is unavailable for nonstream JSON, not inferred from HTTP duration.
The smoke is an API check, not a representative throughput/quality benchmark.

`node --test benchmarks/api-smoke.test.mjs` validates the protocol using only a
local mock server. Importing the script does not send a model request.
