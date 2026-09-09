# HaloClu embedded frontend

Dependency-free HTML/CSS/JavaScript with a compact grayscale layout. HaloClu
is the model-independent frontend name; StrixHaloClusterGLM remains this
repository and its GLM reference deployment. The actual served model identity
is separate from the product name.

`web/assets.go` embeds only `index.html`, `styles.css`, `app.js`, and
`ui-core.mjs`, `downloads.mjs`, `favicon.ico` and the declared PNG branding assets,
including the social card. The gateway lives in `internal/app`, with its entry point under
`cmd/strixglm`. Rebuild using `make build` after changing embedded assets;
serve `.mjs` with a JavaScript MIME type.

Models has stable Download and Local & tested sections: shared refresh, explicit
authenticated/locked states and cleared private details on sign-out. Rendering
the catalog never replaces or pushes the downloader below that long inventory.
The normal page metadata includes description, Open Graph and X card fields;
optional trusted `HALOCLU_PUBLIC_URL` resolves the social image and canonical
URL on the server. See [branding](../docs/BRANDING.md) for deployment limits.

English is the default; Italian is selectable under Options. A non-secret
whitelist of language, text size, density, thinking expansion and sidebar
preferences is stored as `haloclu.preferences`; the old `strixglm.language`
setting is migrated. SSH passwords remain transient; the entered API token is
exchanged for a separate 180-day HttpOnly browser-session cookie. Reloading and
reopening tabs retain authentication until expiry, Forget, cookie clearing or API
token rotation. Conversation records remain server-side. All API requests stay
on the gateway's origin.

Authenticated fetch uses `credentials: same-origin` and `X-HaloClu-Session: 1`.
The raw API token is never saved in web storage. POST/DELETE `/v1/auth/session`
creates/revokes the session; GET reports only authentication/expiry metadata.
Token rotation requires re-entry of the API key. Stale responses are guarded by
an authentication generation, including sessions where JavaScript holds no token.

## Chat, Markdown and attachments

Markdown supports headings, paragraphs, fenced code with language labels and
copy buttons, ordered/unordered lists, quotes, tables, inline code, emphasis
and safe HTTP(S) links. Fence delimiters are not shown around rendered code. Raw HTML is
literal text, never interpreted; no image tags or model-supplied event handlers
are created. Parsing also works incrementally during streaming.

Attach text, Markdown, source code, PDF or other supported files through
authenticated multipart `POST /v1/attachments`. Maximum: eight pending files,
32 MiB each. Preview displays the server's extraction kind, truncation and
warnings. This is bounded text extraction/inspection, not image/audio
understanding, OCR, execution or binary decompilation. Uploading never invokes
the model. The user message carries `attachment_ids`; extracted text is not
duplicated in the browser prompt. Server retention governs attachment IDs.

Export JSON saves the current conversation's raw messages, reasoning, statuses,
settings, attachment metadata, metrics and the actual replay history. Partial
or failed replies are retained for auditing but excluded from replay history.
Export does not contain the tab's bearer credential. Attachment IDs are not
portable without the original server; extracted payloads are not re-embedded.

Conversations use one server-owned ID in Chat and Coding/Pi. Refresh lists saved
records; open restores completed text for Chat replay and retains incomplete
answers for inspection. The gateway saves streaming output independently of the
browser; Stop stream detaches the browser, not the in-flight engine request.
Import JSON creates a new record without executing anything. Stage shared history
explicitly supplies transcript context to the next Pi instruction; it does not
replay tools or restore a native KV checkpoint. Old Pi sessions can be explicitly
recovered from Open in Chat. No automatic reconnect or retry occurs.

Delete removes the canonical record, its linked owned Pi session directories
and unshared attachment files after active work is closed. Original projects,
exports and external backups are not deleted. This is filesystem removal, not
a claim of irreversible physical SSD erasure. See [conversation storage](../docs/CONVERSATIONS.md).

## Coding workspace and advanced fallback

The main Coding panel is a Pi workspace client, using `/v1/workspaces/*`:

1. Inspect capabilities and allowed local roots.
2. Create a local or Remote SSH session. Saved SSH presets contain connection
   metadata, not passwords. SSH host keys must already be trusted by the server.
3. Connect explicitly when needed. Passwords are sent only to Connect.
4. Start Pi explicitly, then submit a prompt only when the backend advertises
   both tool support and the session's prompt capability.
5. Inspect agent events, files, diagnostic presets and custom shell commands.
   File browsing is read-only. Custom shell requires an idle READY Pi session
   and the server's explicit shell capability; each command needs confirmation.
   It uses the existing Pi RPC/SSH executor, not a new agent loop or interactive
   PTY. Protected mode is the local default: source files are copied with current
   edits intact and the original is untouched until explicit verified Apply.
   Direct mode needs a separate deliberate confirmation. SSH commands use the remote account's
   privileges. Review the exact command and target before confirming.

Captured build/test commands run independently on a fresh snapshot after a natural
Pi completion. The UI shows actual reasoning, roots, mode and command receipts.
No configured tests means UNVERIFIED. TEST_PASS means the recorded commands passed,
not a guarantee of correctness. Review modified test definitions separately.
Protected SSH and arbitrary binary/large-repository copying are not implemented.
See [protection and verification limits](../docs/PROTECTED_WORKSPACES.md).

Mutating workspace actions require confirmation. Capability failures are shown
as unavailable, not simulated success. Failed requests are not automatically
replayed. Session close, abort and refresh are separate controls. Polling
retrieves real recorded Pi events; a disconnected transport does not imply
the agent was cancelled.

Advanced / legacy preserves the previously tested isolated coding workflow:
`sandbox_policy: isolated`, `apply: false`, build/test/repair attempts, task
resume, cancel/drain, and explicit apply confirmation. Imported task JSON
cannot weaken isolation or auto-apply. This fallback is not represented as Pi.
Its navigation entry is hidden by default. Options can reveal it with the
non-secret `show_advanced` preference; this does not change API authorization.

## Settings and measured statistics

Generation is contextual: Chat and the opt-in legacy view expose their request
controls. Coding/Pi exposes only new-session reasoning; existing sessions retain
the captured setting. Chat context, output and thinking-budget controls are not
forwarded to Pi. Models, Benchmark, Cluster and Options hide Generation without
resetting its values. Keyboard navigation skips the hidden legacy tab.

Options holds shared interface preferences, gateway authentication and explicit
API admission controls. Token rotation requires confirmation and invalidates
the old gateway credential for new requests. Four server-persisted switches
pause new chat, workspace, legacy coding or operations actions without hiding
inspection and safe cancellation. Listen/backend/model values are read-only;
no network listener or inference rank is reconfigured by the browser.
[Options behavior and API contract](../docs/OPTIONS.md).

`/v1/options` supplies explicit low/high/max reasoning, context/output limits
and timeout. Low is not thinking OFF. A supported thinking budget of zero
forces reasoning closure; it does not imply architectural OFF or equivalent
quality. Expanding the reasoning display does not change computation.

Chat context is the total input + output admission window, not KV resizing.
Auto omits `max_tokens`; the server fits its default to the remaining exact
token budget. Legacy coding context remains source-selection budget plus the
runtime's hard admission guard. Pi session reasoning is chosen at creation.
Context/output settings in the plain-chat sidebar do not silently reconfigure
an existing Pi process. A selectable 32K output cap does not promise completion
within the displayed backend deadline.

Observed TPS uses cumulative real completion tokens / observed HTTP time.
Never characters, words or SSE chunks. Final decode comes from the engine;
chat TTFT is browser-observed. Missing metrics remain unavailable. Legacy task
TPS distinguishes last-attempt decode from observed HTTP rate. Natural stop,
final text and SSE `[DONE]` are required before a reply enters replay history.

Catalog and operations keep source-backed availability and historical evidence
distinct from live measurements. Benchmarks/downloads require confirmation.
The UI never restarts one rank independently.

Models also contains the native Go downloader: public Hugging Face/ModelScope
search, exact-file selection and public HTTPS direct links. Create plan does not
start a download; Start requires confirmation. Durable progress includes bytes,
rate and ETA. Pause/cancel preserves partial files; restart pauses active jobs
until explicit Resume. File acquisition is separate from runtime compatibility,
conversion or model loading. [Sources, validation and resume contract](../docs/DOWNLOADS.md).

## Regression checks

```sh
node --test web/tests/*.test.mjs
node --check web/app.js
node web/tests/browser-smoke.mjs /tmp/strixglm-browser-smoke
```

The PRODUCT-003 baseline recorded 25 frontend unit tests and 16 browser checks
using installed Firefox with a deterministic fake backend. These are historical
counts, not a claim about later UI revisions. No package/browser downloads or GLM calls.
The browser check covers Markdown/XSS, attachment extraction and IDs, JSON
export, language selection, workspace capability gating/files/terminal, confirmed custom shell,
legacy coding/apply/cancel, catalog/jobs and mobile layout. Updated UI checks
must also cover Options, preference persistence, token rotation and API admission
without treating mock results as model-quality evidence.

Explicit read-only live audit (real token read into memory, no inference):

```sh
node web/tests/browser-live-readonly.mjs http://127.0.0.1:18093/ state/api-token /tmp/strixglm-browser-live
```

Use a new evidence directory. Screenshots require hidden credential controls.
The separate `browser-live-actions.mjs` is opt-in and performs one real short
chat plus one legacy isolated task, never apply; run only on an idle engine.
It is not Pi qualification. A chat capped before EOS fails the check.

```sh
node web/tests/browser-live-actions.mjs http://127.0.0.1:18093/ state/api-token /absolute/task.json /new/report --run-live
```
