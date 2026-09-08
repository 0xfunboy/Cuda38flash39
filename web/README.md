# Embedded local frontend

Vanilla HTML/CSS/JavaScript. No build step, third-party assets, external fonts,
CDN, package manager or client telemetry. Serve `index.html`, `styles.css`,
`app.js` and `ui-core.mjs` from the same origin as the Go API. Serve `.mjs` with
a JavaScript MIME type. The browser stores its API bearer token only in the
current tab's memory; reconnect after reload.

## Current profile meaning

The production configuration currently maps **Fast, Balanced and Quality to
the same operational preset**: reasoning `low`, output cap 4096 and 2 repairs.
These three names are compatibility aliases, not three measured quality tiers;
choosing Quality does not enable more reasoning or establish better answers.

The configured context budget 4096 is an **input-selection estimate**, not a
qualification at 4096 actual tokenizer tokens. Actual prompt-token counts come
from the engine after a request. Neither this setting nor the cluster's larger
configured context limit constitutes a long-context reliability claim. The
profile note displays the server's current configuration/qualification status;
use the main product report for measured workload coverage.

The UI connects to its own origin. The same assets work with the compatibility
gateway or the native gateway; select the corresponding local token file. No
endpoint or secret is embedded into the production JavaScript.

The UI uses the real backend routes:

- Chat: `POST /v1/chat/completions`, streamed OpenAI-compatible SSE, with
  `profile`, `messages`, `max_tokens`, and usage requested. Only natural
  `finish_reason=stop` answers with final text **and the SSE `[DONE]` marker**
  enter subsequent chat context. A truncated stream is never treated as success.
- Coding: `POST /v1/coding/tasks`, then `GET /v1/coding/tasks/{id}` while active.
  Build/test commands are user-supplied strings; the backend validates/parses
  them. The request always sets `sandbox_policy=isolated` and `apply=false`.
- Cancellation: `POST /v1/coding/tasks/{id}/cancel`; draining is not represented
  as completed cancellation. Failed polling stops and allows explicit resume.
  `INTERRUPTED` after a gateway restart is terminal, not automatically replayed.
- Apply: `POST /v1/coding/tasks/{id}/apply` with `{"confirm":true}`, only after
  a passing result and an explicit browser confirmation. Backend source-state
  checks remain mandatory; UI gating is not a security boundary.
- State: public `GET /health`, authenticated `/v1/status` and `/v1/models`.

Coding can import a local task JSON specification to reuse CLI/API commands,
explicit context files and isolated independent test fixtures. Import only
fills the form; it never submits, applies changes, imports credentials or
weakens sandbox policy. Clear extra options to remove imported context/test
settings while retaining the visible form fields.

Model output, source paths, errors and diffs are rendered with `textContent`,
never HTML. Missing metrics display an em dash. Chat TTFT and HTTP time are
explicit browser measurements; decode TPS and token usage require server data.
The UI does not claim historical benchmark numbers as current telemetry.
Task TPS is explicitly the last attempt's decode measurement; task wall time
includes all attempts. Optional repairs are limited to the backend's range 0..6.
The chat's context denominator is the server-configured ceiling, not a measured
usable-context result. No speed, success-rate or intelligence scores are
hard-coded into the UI.

## Regression tests

```sh
node --test web/tests/*.test.mjs
node --check web/app.js
```

The unit-test glob is `web/tests/*.test.mjs`, **not** `web/*.test.mjs`.
At the current revision it contains 17 tests. The browser scripts are separate
explicit checks and are not implicitly launched by that unit-test glob.

Optional actual-browser test with installed Firefox and geckodriver:

```sh
node web/tests/browser-smoke.mjs /tmp/strixglm-browser-smoke
```

That smoke test starts an ephemeral, deterministic **fake API**, not GLM.
It checks authentication, SSE/UTF-8, XSS-safe rendering, coding results and
repair attempts, apply confirmation, cancellation, live-state rendering,
mobile overflow and memory-only credentials. It records screenshots and
cleans up its own server and browser. It downloads nothing and sends zero
real model requests. A separate live product test is still required.

Read-only live browser check (token is read into memory, never printed or
captured in screenshots; no chat/coding requests are submitted):

```sh
node web/tests/browser-live-readonly.mjs http://127.0.0.1:18093/ state/api-token /tmp/strixglm-browser-live
```

It verifies actual health for both ranks, model identity, memory/GPU telemetry,
JavaScript MIME type, API authentication, mobile layout and missing/structured
status handling. Runtime profile qualification status is shown as reported;
an unqualified configuration is never presented as an established winner.

An explicitly opt-in live action test sends **one real short chat and one real
coding task**, with up to the task's configured repairs. Run it only when GLM
is idle; it refuses a busy preflight and never applies a patch:

```sh
node web/tests/browser-live-actions.mjs http://127.0.0.1:18093/ state/api-token /absolute/frozen/task.json /tmp/strixglm-frontend-e2e --run-live
```

This test exercises the real UI's file import, submit and streaming handlers,
checks natural chat completion and measured metrics, verifies the final build
and tests pass, and checks original editable files remain byte-for-byte intact.
It records the task ID and raw evidence; a failure is not automatically retried.
