# GLM coding qualification — final product verdict

## Workspace revision STRIX-PRODUCT-003 (2026-09-09)

The user-supplied conversation was audited offline, preserving its actual final
C blocks without repairs. Small-input LOW examples were correct, but integer
overflow/input parsing were unsafe. The MAX sieve reproduced bounds/allocation
errors. Its report prose included an unsupported budget-only conclusion and
a wrong B cost/correct solution (238.153575s, not204s). This is not a new model
ranking. Exact audit/raw are local-only at
`/home/funboy/ai-exp/reports/moe-cluster/STRIX-PRODUCT-003/ANSWER-QUALITY-AUDIT.md`;
private transcripts and deployment state are not included in this public source.

Product changes: safe Markdown/code copy, conversation export, bounded text/PDF/
DOCX/archive/binary attachments, English default with Italian selection, actual
Pi0.85.1 RPC Local/SSH workspaces, persisted host metadata, file browsing and
confirmed shell diagnostics. The original agent loop remains only in Advanced.

Function tools use the same runtime tokenizer plus paired raw completions:
generated text is compared before allocating one set of tool IDs. Two actual
GLM requests passed: read-call envelope followed by tool-result interpretation,
both naturally concluded. Their HTTP times1.304/1.634s are short protocol
smokes, not decode TPS or model-quality benchmarks. Tool turns are buffered;
ordinary chat remains streamed. No generated code is executed by the adapter.
Pi performs its own schema validation before executing tools.

Local Pi isolation was checked with actual RPC/bash: a listener on host TCP
was unreachable; model discovery succeeded solely through a private Unix
socket bridge. Effective scope:2GiB RAM, swap0,128 tasks,200% CPU. No root bearer
credential is exposed to Pi. Remote execution instead has the connected SSH
account's privileges and must not be represented as a remote sandbox.

Evidence: `/home/funboy/ai-exp/reports/moe-cluster/STRIX-PRODUCT-003/`.
Delivered on18093; source-reproducible binary SHA-256
`31b6be8cb344d97edea19fce10965f03adbf894e4f14428a008513206897c6cc`.
Only the gateway restarted. Both rank and original coordinator InvocationIDs
are identical to the preserved snapshot; health is good, idle and unpoisoned.
The preview and test Pi scope were closed. PRODUCT-002 rollback is retained.

- 124 Go tests plus26 subcases PASS with race detector, no skips; `go vet` clean.
- 40 Node tests PASS, including25 frontend tests.16 Firefox mock checks PASS.
- 7 GET-only deployed Firefox checks PASS; no generation during that check.
- Real Pi smoke:4 model turns,3 tool calls (read/write/bash), natural final
  answer,49.387s. A separate independent oracle compiled the **unedited** C11
  source with warnings-as-errors and ASan/UBSan:213 cases PASS. This is one
  bounded task at low, not a general coding-quality qualification or proof
  that low exceeds max. The richer task specification also differs from the
  user's original short prompt.
- Pi's own bash printed example results without assertions. Its final answer
  additionally claimed a missing-argument test absent from that command. The
  independent oracle did test missing arguments and passed. We do not silently
  upgrade the accuracy of the agent's original test-reporting claim.
- Real Firefox Pi/file/shell check PASS: selected actual recorded session,
  rendered its answer, opened nth_prime.c, ran exactly `printf workspace-shell-ok`
  with confirmation and exit0. No GLM request from that browser check.
- Local RPC, Unix bridge, blocked host TCP, effective scopes and full owned-scope
  cleanup were tested. Password broker and SSH extension startup were tested
  without connecting to an actual host. **SSH login and a remote coding task
  remain unqualified on the user's chosen hosts.** No remote install was made.

Raw includes `go-tests.jsonl`, `node-tests.log`, `workspace-tests.log`,
`attachment-tests.jsonl`, `pi-events.json`, `pi-independent-prime-check.json`,
`browser-workspace-live/`, `browser-deployed-readonly/`, `SOURCE-PIN.json`,
`deployment.json` and `rollback/`. A native18095 runtime cutover was not performed;
its tools flag remains off until that transport is separately exercised.

## Console revision STRIX-PRODUCT-002 (2026-09-08)

Deployed on18093. Grayscale/collapsible UI, real cumulative streaming usage,
explicit reasoning/context/output, source-pinned model inventory and confirmed
asynchronous operations. The GLM engine, weights and both rank invocation IDs
were preserved; only `strixglm.service` restarted. This section does not change
the model-quality conclusions or historical measurements below.

- 89 Go tests with race detector, `go vet`, 36 Node tests and 12 Firefox mock checks PASS.
- Real Firefox preview: one naturally concluded RAII chat and one scheduling C++
 task, PASS first attempt with independent fresh-snapshot compile/test. Original
 files unchanged. Task16.955s,355 completion tokens,24.572 decodeTPS; this single
 functional rerun is not a new speed qualification.
- Runtime token admission matched engine prompt usage (38 tokens); 48 positive
 streaming usage events in the explicit-high/budget0 RAII smoke. One confirmed
 `api-smoke` operation completed PASS through the actual async job manager.
- Native thinking budget 0 produced 202 completion / 0 reasoning tokens and natural
 stop. Budget128 produced229/12 and natural stop, so that second case did **not**
 exercise the128 boundary. Optional budget support is verified, not equal-quality
 certification or proof of every budget value.
- Real deployed Firefox GET-only check: 7 checks PASS, including options, catalog,
 operations, live telemetry and desktop/mobile layout. No model generation in
 this deployed read-only test. Preview18096 was stopped after deployment.
- New negative tests reject unknown thinking controls, `medium`, conflicting caps,
 invalid stop/top_p, non-stream stream_options, template overrides and total
 context overflow before inference. Tokenizer failure never dispatches a rank.
- Downloads were tested with local HTTP fixtures only: range resume, integrity,
 symlink/hardlink, free space, no-overwrite and cancellation. No actual model
 download was needed or started.19 catalog models /177 components; NODE01 stat
 is not SHA verification, NODE02 files were not re-inspected. Other-model
 runtime switching is blocked, not implemented by a decorative Load button.

Raw: `/home/funboy/ai-exp/reports/moe-cluster/STRIX-PRODUCT-002/`;
`FINAL.md`, `deployment.json`, `SOURCE-PIN.json`, `preview-smoke-summary.json`,
`thinking-smoke-summary.json`, `browser-preview-actions/frontend-e2e.json`,
`browser-mock-final/`, `browser-deployed-readonly/` and test logs.
Rollback binary/config are in `rollback/`; the original paired snapshot remains
`state/legacy-handoff-20260908.json`. No runtime cutover or new math qualification.

PRODUCT-002 limits (historical): response cap includes thinking, Auto up to16,384, explicit up
to32,768, total context65,536, backend deadline600seconds. A selectable context
is not quality-qualified. At that revision Pi native tools/Responses were unsupported; the
coding task API is a separate verified-diff workflow. No further automatic tests.

## Preserved model qualification (STRIX-PRODUCT-001)

Evidence snapshot: **2026-09-08**, native lifecycle and original rollback completed.
**READY for the measured compact C++ workflow**, not universal programming
reliability, near-SOTA parity, or long-context qualification.

## Runtime and method

Two GMKtec EVO-X3, each Ryzen AI Max+395 / Radeon 8060S / 128 GB UMA. Target:
GLM5.3-Flash-CIRU-STRIX-IU4 hybrid W4, TP2/PP1, RCCL Socket over USB4,
DFlash2 k5/local0, safe-prefill, canonical MoE, stable routing/coherent tiles,
correct F1, prefix cache off. Engine and weights were not changed for this
coding comparison. Pins/layout are in [runtime/manifest.json](runtime/manifest.json).

The original coding/profile/context requests went through the **new Go product**,
with the preserved raw inference API as backend. The separately identified
task10 below ran through the native coordinator/gateway; the earlier nine
fixture successes are not retroactively native results. A single native coding
result alone does not finish coordinator/cutover qualification; the separate
API/browser/restart/rollback checks below complete that evidence. Coding tasks used
fresh private source snapshots; frozen fixture repositories remained unchanged.
The separately identified explicit-apply test changed only its disposable copy.
Correct goldens and independent hidden tests
passed before bug injection; every selected buggy fixture compiled successfully
and failed a functional test. A delivered PASS was compiled/tested again from
another fresh snapshot. Hidden tests/goldens were absent from initial prompts.

Primary metric: task wall time per correct solution, including failed task time
within a comparison. Decode TPS is `(completion_tokens - 1) / generation_seconds`
and includes reasoning tokens; it is **not** final-code throughput. Client TTFT
measures the first streamed token, including reasoning. Acceptance is the engine's
draft acceptance rate. Medians below are across task attempts, not three repeated
speed measurements. Missing transport telemetry is not reported as measured zero.

Raw report root, abbreviated **RAW** below:
`/home/funboy/ai-exp/reports/moe-cluster/STRIX-PRODUCT-001/`.
Full requests, SSE, metrics, checks and verified patches are also retained under
`state/tasks/<task_id>/` outside version control.

## Six-task low baseline

One request per task, temperature 0 / seed 1, reasoning `low`, output cap 4,096,
timeout 600 seconds, up to two repairs available. All finished naturally on the
first attempt; no repair was needed and no candidate was manually corrected.

| Task | Result | Wall to correct | Decode TPS | Task ID |
|---|---|---:|---:|---|
| Escaped settings parser | PASS | 21.67 s | 23.22 | `2f499c897379b8f43579bd71bb2bf78e` |
| Expiring LRU cache | PASS | 22.76 s | 27.36 | `27587dc2670acc67e7e262c2b88df760` |
| Scheduling interval subtraction | PASS | 17.35 s | 23.94 | `4406e0b2809b69681c03651089cd080d` |
| Atomic inventory import | PASS | 29.53 s | 23.42 | `1781e8a31bc2bb0141ac03603a8f5fbd` |
| Catalog/cache refactor | PASS | 22.55 s | 28.32 | `5d6f136e785105a984e3a47fcff8bc6b` |
| Queue/worker exception recovery | PASS | 7.04 s | 27.52 | `230b40da3898543cffba8a9e935a9337` |

Aggregate: **6/6 PASS**, 6 model calls, 0 repairs, 120.90 seconds total;
**20.15 seconds per correct solution**, 178.67 correct solutions/hour for this
particular serial fixture workload. Median task wall time: 22.11 seconds.
Median decode: **25.65 TPS** (23.22–28.32 across different tasks), median measured
HTTP completion throughput: 21.55 TPS, median client TTFT: 2.239 seconds,
median draft acceptance: 67.52%. Token totals: 426 reasoning + 2,157 final =
2,583 completion; 4,634 prompt tokens across six requests.

Sources: `RAW/coding-low/*.receipt.json` and `RAW/coding-low/summary.json`.
These workload-dependent TPS are not a directly comparable replacement for the
historical 109/256 benchmark's 24.85 decode / 23.67 HTTP TPS.

### Harder low tasks subsequently completed

| Task | Result | Calls / repairs | Actual prompt tokens | Wall to correct | Decode TPS | Task ID |
|---|---|---:|---:|---:|---:|---|
| 07 incremental binary-frame decoder | PASS | 1 / 0 | 1,230 | 28.94 s | 31.00 | `9ca30543552d8d888b73a6187e31b8d7` |
| 08 tenant settings / dependency cache | PASS | 1 / 0 | 1,898 | 31.98 s | 30.89 | `755e5cd6da6672c32b58da2393d553d0` |
| 09 asynchronous request coalescing / reentrancy | PASS | 1 / 0 | 1,576 | 49.467 s | 20.41 | `9005222616169c72784decd183c90e31` |

This brings the distinct functional repository fixtures to **9/9 PASS on the
first model attempt**, using JSON source packaging and low reasoning. All three
additional tasks had correct goldens and functional-failing, successfully
compiling buggy variants frozen before model use. Task09 uses a deterministic
event-loop loader, not actual threads or a real network service. Raw:
`RAW/extended-low/07-*.receipt.json`, `RAW/extended-low/08-*.receipt.json` and
`RAW/coalescing-low/09-request-coalescing-cache--low.receipt.json`.
Task09 used a 4,096-token cap and three available repairs, but used none; its
response recorded `envelope_normalization=none`. It fixed completion ownership
and state-before-callback ordering in actual C++, not just the response envelope.

At this point in the chronology no real-model success after a build/test failure
had been observed. The native feature task below subsequently demonstrated one;
the original first-pass successes are not reclassified as repair successes.

### Native task10: algorithm implementation and real test-guided repair

The incremental UTF-8 feature finished **PASS in two calls / one automatic repair,
152.446 seconds including the failed attempt and fresh independent verification**.
It extends an ASCII-only legacy service with a real stateful decoder, rather than
repairing another pre-existing one-line bug. Reasoning was low, output cap 4,096,
two repairs allowed, temperature 0 / seed 1; both calls stopped naturally.

| Attempt | Build / functional test | Actual prompt tokens | Reasoning / final tokens | HTTP time | Decode TPS | Client TTFT | Acceptance | Envelope normalization |
|---|---|---:|---:|---:|---:|---:|---:|---|
| Initial implementation | PASS / FAIL | 910 | 1,209 / 925 | 105.106 s | 20.803 | 2.575 s | 54.58% | One outer closing brace |
| Automatic repair | PASS / PASS | 1,800 | 238 / 940 | 45.885 s | 28.554 | 4.664 s | 75.30% | None |

The first response's complete `files` map lacked only its outer closing brace.
Lossless envelope normalization produced compilable **unchanged C++ bytes**,
which then genuinely failed `whole.feed(encoded)==scalars`. In that implementation,
the `0xED` lead-byte branch initialized the scalar accumulator to zero, losing
the lead's payload bits for valid scalars such as U+D7FF. The automatic next
request contained current source and the failing assertion only: no golden,
oracle implementation, expected scalar value or human-supplied fix hint.

The model repaired scalar initialization using the appropriate lead-byte mask.
The second generation compiled and passed the frozen oracle, then passed another
build/test from a fresh snapshot. Its final source is byte-identical to the
verified artifact, SHA256
`2ab3f3b157873db9ef1c57feb638e2d6aec2a77c9bf4b68fba887d1ac05a1b30`.
The hidden oracle SHA256
`ccaf6cfbbfc0ef5bcc9babe13b7d99ad6f09c91943f8d026a9c0106d9f5e53df`
and all initial source hashes match the pre-inference freeze. The first candidate
remains recorded as a functional failure; normalization is not credited as a
code fix. Neither generated candidate was manually edited.

Readonly logic audit found no hardcoded answers, disabled checks or changed
public API. The implementation validates each available continuation before
accepting an incomplete prefix, enforces overlong/surrogate/scalar-range bounds,
stages all decoding before committing pending bytes/counters, and preserves
finish/reset/closed behavior. The independent encoder oracle covers 78 scalar
values, every byte split of their encoded stream, 13 chunk strides, malformed
prefixes and transactional retry/lifecycle cases. This is a useful algorithmic
feature result, not exhaustive Unicode fuzzing, allocation-failure qualification,
or a performance benchmark of the generated decoder.

Distinct functional fixtures now total **10/10 eventual PASS: nine first-pass
successes on the compatibility backend and one repaired success on native**.
This demonstrates a real model-driven repair cycle on one task, not a general
repair success rate or matched native-versus-compatibility speed comparison.

Raw: `RAW/native-feature/10-incremental-utf8-feature--low.receipt.json`; task
`d1a330a80aefd7531f30cf0090955b1a` under `state/native-gateway/tasks/`, including
both request/response/check pairs, `independent-check.json`, `inputs.json` and
the verified source/patch. Golden, legacy failure and oracle pins:
`RAW/suite-utf8-v1/manifest.json`.

### Native coding reconfirmation, API and browser

The Go gateway at `127.0.0.1:18095` used the native whole-pair coordinator at
`127.0.0.1:18094`. Two original coding fixtures were rerun as native confirmation;
the parser was then used for live product/API and browser checks. All four
completed on the first model call, with natural stop, no envelope normalization,
successful compile/tests and fresh independent verification.

| Native run | Result | Task wall | Actual input tokens | Reasoning / final tokens | HTTP time | Decode TPS | Client TTFT | Acceptance |
|---|---|---:|---:|---:|---:|---:|---:|---:|
| 03 interval subtraction | PASS | 16.582 s | 594 | 76 / 279 | 15.744 s | 25.184 | 1.684 s | 66.34% |
| 05 catalog/cache refactor | PASS | 23.692 s | 872 | 44 / 495 | 22.469 s | 26.741 | 2.343 s | 76.07% |
| Parser, explicit-apply API | PASS | 21.658 s | 675 | 77 / 361 | 20.782 s | 23.158 | 1.905 s | 60.93% |
| Parser, real browser after cancellation | PASS | 22.358 s | 675 | 77 / 361 | 21.501 s | 22.335 | 1.933 s | 60.93% |

The dedicated 03/05 confirmation is **2/2 PASS**, 40.275 seconds total,
20.137 seconds/correct, median decode 25.962 TPS, median TTFT 2.014 seconds
and median acceptance 71.21%. These are two different tasks, not three repeated
performance measurements. Task IDs:
`4ab4e32cd6bbbd94157a624a49198a51` and `4f26d6788267f96316c612161ee40c70`.
Raw: `RAW/native-confirmation/*.receipt.json` and `summary.json`.

The live API exercise finished **20/20 named product checks PASS**. It covered
authentication, models listing, rejecting malformed chat before entering ranks,
async task submission, isolated C++ build/tests, source unchanged before apply,
explicit confirmation required, application of verified changed files only, and
unchanged frozen source. A second active generation was cancelled: the task
entered `draining`, the model call completed without application, then the task
became `CANCELLED`; both ranks were healthy/idle and both source copies unchanged.
Cancellation here is **safe drain**, not immediate interruption of GPU work.
The cancelled request's 19.265-second task wall is not a measured cancel latency
and it is not counted as a coding failure or success. Raw:
`RAW/native-product-e2e/result.json`, PASS/apply task
`e4a43f5863076af0346f9a32ea4f55ca`, cancel task
`57de3f7209e521515119bc8c1539116b`.

The real Firefox/browser exercise subsequently finished **PASS at 12:54:49 UTC**,
after the API cancellation/drain had finished at 12:53:09. A chat request streamed
a naturally concluded two-sentence RAII explanation: 70 completion tokens,
19.86 displayed decode TPS, 429 ms displayed TTFT and 3.76 seconds HTTP. The
browser showed final text and live measured metrics; this is a short prose/API
smoke, not a broad writing-quality benchmark. The imported parser task then
showed a real PASS with diff/build/tests/attempts, and left original sources
unchanged with `applied=false`. These two successful model requests demonstrate
usable generation **after the previous API cancellation**, not an independently
tested browser Stop/cancel action. Raw:
`RAW/native-frontend-e2e/frontend-e2e.json`, `chat.sse` and the retained browser
screenshots; coding task `0dd437ee6d4eb4bcd5f7af8daa973776`.

The distinct functional task count remains **10**, not 14: these native
reconfirmations and API/browser checks reuse existing fixtures. Four distinct
fixtures have now passed natively (01, 03, 05 and 10); task10 is the only new
algorithm feature and the only demonstrated successful real-model repair.

### Native memory observations and missing fault telemetry

Values below are whole-node `MemTotal - MemAvailable` snapshots, not model RSS,
dedicated VRAM, model-weight size, or peak memory during generation. Linux reports
approximately 122.19 GiB total usable memory on each node.

| Native workload | NODE01 before → after | NODE02 before → after |
|---|---:|---:|
| UTF-8 feature plus repair | 102.76 → 103.23 GiB | 101.71 → 101.83 GiB |
| 03 interval subtraction | 103.05 → 103.22 GiB | 101.81 → 101.84 GiB |
| 05 catalog/cache refactor | 103.22 → 103.27 GiB | 101.85 → 101.85 GiB |

These same receipts report both ranks reachable/healthy before and after.
The snapshots do not attribute small deltas specifically to inference, distinguish
all UMA consumers, or measure a leak. **Major/minor page faults: N/A**, absent
from this campaign's receipts; zero faults must not be inferred. Source telemetry
calculation is in `main.go` (`nodeStats`), raw node observations in each
native-feature/native-confirmation `cluster_before` / `cluster_after` object.

## Matched reasoning subset: low / high / max

The discriminating subset is the same escaped parser and atomic inventory
import. All six usable comparison runs share an **8,192 completion-token cap**,
600-second per-call timeout, temperature 0 / seed 1 and two available repairs.
Prompt lengths are 675 and 800 tokens respectively. Transport failures are
preserved separately below and excluded from model-quality comparisons.

| Effort | Correct | First-pass | Calls / repairs | Total wall | Seconds/correct including failures | Correct/hour | Median decode | Median TTFT | Median acceptance |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| low | 2/2 | 2/2 | 2 / 0 | 47.94 s | **23.97 s** | 150.19 | 24.89 TPS | 2.032 s | 62.89% |
| high | 2/2 | 2/2 | 2 / 0 | 137.62 s | 68.81 s | 52.32 | 19.88 TPS | 2.170 s | 52.69% |
| max | 0/2 completed | 0/2 | 2 / 0 | 776.56 s | N/A: no correct completion | 0 | 21.27 TPS | 2.049 s | 50.16% |

| Effort | Reasoning tokens, sum | Final tokens, sum | Completion tokens, sum | Prompt tokens, sum |
|---|---:|---:|---:|---:|
| low | 243 | 807 | 1,050 | 1,475 |
| high | 1,722 | 887 | 2,609 | 1,475 |
| max | 16,384 | 0 | 16,384 | 1,475 |

Both max runs ended `finish_reason=length`, with all 8,192 tokens spent on
reasoning and no final code. They are **INCOMPLETE**, not failed compiled
solutions and not evidence that the model cannot solve the problems. High
achieved the same two successes as low, taking approximately 2.87× as much
time per correct solution. This small sample currently favors low; it does not
establish a universal ranking or justify a separate higher-quality preset.

**Medium is not an independent supported effort in the pinned template.**
`artifacts/ciru/config/chat_template.jinja:2` in the preserved engine accepts
`low` and `high`; other values, including `medium`, map to `max`. A label of
medium must not be presented as a separately measured reasoning mode. No engine
template was changed to invent medium support.

### Selected operational profiles: three honest aliases

The current `config.json` and `config.native.json` select the same compact
configuration for all three frontend labels. There is no measured evidence here
for claiming that the label QUALITY produces better code than FAST.

| Profile label | Effective reasoning | Estimated input-selection budget | Output cap | Maximum repairs |
|---|---|---:|---:|---:|
| fast | low | 4,096 | 4,096 | 2 |
| balanced | low | 4,096 | 4,096 | 2 |
| quality | low | 4,096 | 4,096 | 2 |

The input budget is a **byte-based estimate**, using a three-bytes-per-token
selection heuristic with overhead. It is not a tokenizer count, a guaranteed
4,096-token prompt limit, or evidence of successful coding at 4K actual input.
The largest passing integration fixture remains 2,575 actual prompt tokens.
The research override ceiling of 50,000 is also not a qualified context size.
The old profile mappings retained in earlier raw receipts describe the
configuration at that time, not this selected alias configuration. Native
native validation is complete; permanent adoption was deliberately not performed.

Exact records used for this comparison:

| Task / effort | Receipt relative to RAW | Task ID |
|---|---|---|
| Parser low | `profiles/01-escaped-settings--low.receipt.json` | `dbab86d4194aebcf0023f311e86b3054` |
| Parser high | `profiles/01-escaped-settings--high.receipt.json` | `eaeb259cf1b8bfa22def7f2d5ed1f1ef` |
| Parser max | `profiles/01-escaped-settings--max.receipt.json` | `92686fc8b1ce627dd84b9baead369987` |
| Inventory low | `profiles/04-inventory-import--low.receipt.json` | `3654ff0a50c56b746cc8b0303cca7c02` |
| Inventory high, recovered pair | `profiles-recovered/04-inventory-import--high.receipt.json` | `13f355ad9d56aac3ee29dcab62d7aa47` |
| Inventory max, recovered pair | `profiles-recovered/04-inventory-import--max.receipt.json` | `0f0afa73f2b39a8a89ac4775b4f7bc36` |

Negative infrastructure records, **not silently dropped**: the original
`profiles/04-inventory-import--high.receipt.json` was BLOCKED after a
60.02-second response-header timeout (`bf055c047d51cd04e6e48a67b37d5b9b`). The
following max attempt was BLOCKED by HTTP 503 / poisoned pair / rank-1
RemoteProtocolError (`b89f7d6a135d14e28679a7a0059a5f90`). Their token/TPS values
were unavailable. Whole-pair recovery preceded the replacement measurements;
these setup/transport events are not quality failures attributable to high/max.

## Independent patch audit and scope

All six low patches were reviewed against public contracts and frozen goldens.
No hardcoded outputs, disabled tests, assertion bypasses or newly introduced API
regressions were identified. Changes are small and semantically match the golden
fixes; pricing also added appropriate direct header includes. All six independent
fresh build/test receipts passed and hidden-test SHA256 matched the frozen oracle.

One complete six-task oracle execution evaluates **3,589 `CHECK` assertions**:
parser 115, TTL 109, intervals 3,241, inventory 21, pricing 92, queue 11, plus
the queue callback's explicit ID guard. The interval count includes 3,200
unit-cell comparisons, 36 structural checks and five fixed checks. These are
**not 3,589 independent coding tasks**; repeated verification does not multiply
the independent evidence. Manifest `cases` fields mix scenarios and assertions
and must not be summed as a benchmark score.

Limits: small synthetic C++17 repositories, mainly one-line bug fixes in the
original six-task set, ordinary single-thread operation. Task10 adds an actual
decoder implementation, but these results do not establish near-SOTA coding intelligence,
large production-repository competence, TypeScript/Rust quality, persistent
queue durability, concurrency safety or arbitrary initial corrupted state.
The pre-existing golden `Cache` is implicitly copyable despite stored list
iterators; copy semantics were not qualified and can be unsafe. That is an
uncovered baseline design issue, not a regression introduced by a GLM patch.

## Context scaling — completed measured results

These are real service-registry source dependencies and distinct policy
contracts, not random padding. Every service must be integrated correctly.
Nevertheless, this measures **mechanical cross-file integration sanity**, not
general long-context software engineering. Nominal labels are not token counts.

| Nominal fixture | Actual prompt tokens | Current result | Calls | Task wall |
|---|---:|---|---:|---:|
| 1K | 678 | PASS | 1 | 13.65 s |
| 2K | 1,306 | PASS | 1 | 7.81 s |
| 4K | 2,575 | PASS | 1 | 14.81 s |
| 8K | 5,107 first / 5,138 repair | FAIL | 3 | 84.77 s |
| 16K | 10,191 first / 10,222 repair | FAIL | 3 | 144.66 s |
| 32K, 63 policies | 20,360 first / 20,391 repair | FAIL | 3 | 315.04 s |
| Expanded, 95 policies | 30,529 | INCOMPLETE | 1 | 331.01 s |

The six original context bands therefore contain **three PASS and three FAIL**.
At 5K, 10K and 20K actual context, failures occurred before compilation: empty files
objects and malformed/truncated final JSON despite natural stop. One inspected
5K output additionally referenced the wrong service namespace for a factory;
the issue cannot be called purely cosmetic JSON formatting. No malformed output
was repaired manually and then counted as model success. At this snapshot the
largest passing JSON-format integration task is **2,575 actual prompt tokens**;
it is not a universal context ceiling or an impossibility claim about the hardware.

Raw: `RAW/extended-low/*.receipt.json`, source/task pins in
`RAW/suite-latest.json`, private preparation receipts under the referenced suite
directories. Task `296f29a5f3927b48657d9b5adbf183d8` records the completed
20K-context failure. The completed 95-policy expanded run is detailed below.

### Plain source packaging candidate — rejected

This candidate changed how **input source files** were packaged in the prompt,
from a JSON map to explicit plain source blocks. It did not change target weights,
source bytes, or the requirement to return final JSON.

| Task | Result | Calls | Actual prompt tokens | Wall | Principal failure/result |
|---|---|---:|---:|---:|---|
| Parser | FAIL | 3 | 653 / 694 | 48.68 s | Complete code but missing the single outer JSON closing brace |
| 5K integration | FAIL | 3 | 4,678 / 5,394 / 5,398 | 82.78 s | Wrong factory names/calls; compilation fails after feedback |
| 10K integration | FAIL | 3 | 9,250 / 9,281 | 177.81 s | Invalid/empty files envelope; never reaches compilation |
| Tenant settings | PASS | 1 | 1,611 | 54.77 s | Slower than the 31.98-second JSON-source baseline |

Raw: `RAW/plain-context-low/*.receipt.json`. In all three parser attempts,
adding just the missing outer brace permits ordinary JSON decoding to code
**byte-identical** to the qualified parser, SHA256
`e2b6c549950bf021e6e073138e9320f53d6a49711411ce6a3133a08d497cc8f0`.
That is a diagnosis of an envelope failure, not a retroactive model PASS or a
manual C++ repair. The original three-attempt FAIL remains in the record.

### Lossless envelope normalization and reconfirmation

The product now tolerates a direct allowed-path-to-string map, or a complete
`files` map missing **only one outer closing brace**. It records which envelope
normalization was used. It does not alter decoded source bytes, invent missing
source, close an unfinished source string, double-unescape code, extract code
from reasoning, or accept a capped/unfinished generation. Natural-stop checks,
path allowlists and mandatory independent compile/tests still apply.

Offline regression tests explicitly cover byte preservation, null file values,
unfinished strings/maps, unexpected outer keys, unknown/traversal paths and
empty maps. `envelope_test.go` contains these guards; the agent rejects
`finish_reason=length` before parsing any source.

Fresh JSON-input reconfirmation through the new normalizer passed **2/2**:
parser 21.84 seconds / 22.88 decode TPS and tenant settings 33.95 seconds /
29.33 decode TPS, one model call each. Both live reconfirmations recorded
`envelope_normalization=none`; the alternative-envelope behavior is supported
by its source-preservation tests, not falsely claimed to have fixed these two
already-well-formed responses. Raw: `RAW/envelope-low/*.receipt.json`.

### High reasoning did not recover the failed context bands

| Context | Result | Calls | Wall | Actual outcome |
|---|---|---:|---:|---|
| 5,107 / 5,139 prompt tokens | INCOMPLETE | 2 | 311.20 s | First response has unexpected outer keys; repair reaches 8,192-token cap, with zero reasoning tokens reported |
| 10,191 prompt tokens | INCOMPLETE | 1 | 318.97 s | 8,192 reasoning tokens, no final code |

These runs used the same 8,192 output cap and did not produce a qualifying
solution. The first 5K formatting failure followed by a capped response is not
a successful repair. Raw: `RAW/context-high/*.receipt.json`, task IDs
`df40c869e9f52a14ed3fe5159f6fa57e` and `42aa7f9b3d5fd222b1656295e2705cea`.

### Indexed context candidate — FAIL

The deterministic symbol/index-assisted JSON-input candidate ended **FAIL after
three calls / two repairs, 104.32 seconds** on the same 15-policy task. Actual
prompt lengths were 5,989, 6,949 and 6,763 tokens; output cap 4,096.

Both first responses used an accepted `direct_file_map` envelope, with decoded
source bytes preserved. They nevertheless failed compilation: wrong
namespace/factory associations, including `service_007::policy_233` on the first
call and `service_012::policy_492` after compiler feedback. The final attempt
contained an invalid extra top-level JSON object and did not reach compilation.
Thus normalization worked as intended on two responses but did **not** make
their code correct, and the repair loop did not reach a verified solution.

Raw: `RAW/indexed-context-low/context-8k--low.receipt.json`, task
`5f011b97e00677648c5b1edff4ed265e`. None of these responses was manually fixed
and credited as a model PASS.

### Near-32K actual input — INCOMPLETE, not qualified

The separately frozen 95-policy fixture supplied **30,529 actual prompt tokens**
(99,009 useful source characters), not exactly 32,768. Low reasoning, output
cap 4,096 and zero repairs were used for this one bounded probe. It reached the
cap after 3,350 reasoning plus 746 final tokens, `finish_reason=length`:
**INCOMPLETE**, no delivered/compiled candidate and no claim of functional PASS.

Measured client TTFT was **87.198 seconds** (server 87.146), task wall 331.011
seconds, HTTP 331.008 seconds, decode 16.795 TPS, HTTP completion throughput
12.376 TPS and acceptance 36.34%. High throughput or partial final text would
not qualify an unfinished solution; truncated content was not normalized into
code. This is a workload/budget result, not a physical context limit.

Raw: `RAW/context-actual32k/context-32k-expanded--low.receipt.json`, task
`138cb4080b7cabff13ec498aeea0df02`; source and oracle pins:
`RAW/suite-extra-v1/manifest.json`.

## Lifecycle, regressions and final handoff

Native whole-pair restart **PASS**: owner1788871223396 became1788872127009,
with new nonces and InvocationIDs on **both** ranks. A real CLI streaming answer
and a separate nonstream OpenAI JSON completion passed after the restart.
Gateway/coordinator admission was drained before paired lifecycle actions.
Original whole-pair restore **PASS**, followed by a real request through the
restored product18093. Original raw18091 and coder18092 remain available.
Native services18094/18095 are installed but inactive; no permanent cutover or
automatic boot enablement was performed. This compatibility handoff explicitly
uses the old raw coordinator; the independently validated native path is all Go
above the pinned Python inference engine.

Exact operation/identity evidence: `RAW/NATIVE-HANDOFF.md`,
`RAW/NATIVE-RESTORE-AUDIT.md` (three original units active, actual infinity
retention, unchanged argv/environment/properties and mathematical identities),
`native-before-restart.json`, `native-after-restart.json`,
`native-cli-postrestart-confirmed.txt`, `native-json-api-smoke/`,
`restored-json-api-smoke/`, `runtime-verify-final.json`, `product-final-*.json`.
The original consumed snapshot and fresh handoff snapshot are private files
`state/native-final-20260908.json` and `state/legacy-handoff-20260908.json`.
An immediate connection-refused before a gateway listener was ready was a
setup event with zero submitted requests; readiness was checked before the
actual CLI smoke. No ambiguous POST was replayed and no weight rehash occurred.

Local release regression suite: **67 Go tests**, race detector and vet PASS;
**32 Node tests PASS**; **10 mock Firefox checks PASS**, separated from the real
browser/model results. Scope includes sandbox no-network/no-home, resource caps,
hidden staging symlink escape refusal, apply stale-state/rollback/mode guards,
paired ownership/poison/drain, CLI application waiting and explicit chat caps,
SSE truncation and token non-persistence. Resume treats an interrupted task as
terminal and never automatically resubmits it. Raw: `release-unit-tests.log`,
`release-race-tests.log`, `release-vet.log`, `release-go-test-list.txt`,
`release-resume-guards.log`, `frontend-mock-release/`.

All requested acceptance categories have been exercised through the product:
algorithm, parser, C++ compilation, multi-file repository, genuine logical
repair, chat JSON/SSE, coding API, browser, whole-pair restart, cancel and sandbox.
The ten distinct compact coding fixtures used11calls and383.727seconds total
(38.373seconds/correct), spanning compatibility and native execution; this is
not a matched runtime speed comparison. The six-task and two-profile comparison
tables above remain the clean workload-specific performance references.

Residual limits: small C++17 projects with installed system dependencies;
text-only snapshots; no general TypeScript/Rust/package-manager or IDE-tool
qualification; explicit allowed paths and tests required. Cancellation drains
rather than preempts GPU work. Applying saved tasks after a gateway restart is
deliberately refused; interrupted applications require receipt reconciliation.
Quality remains dependent on meaningful independent tests. The context probes
are complete and their negatives preserved: compact low is selected under all
three profile labels, without claiming a general hardware/context ceiling.
No further automatic benchmark or research branch remains pending.
