# PRODUCT-007: persistent workflows, protected coding and downloads

Reference deployment verified on 2026-09-09. The GLM target, quantization,
coordinator, paired rank identities, token and configuration were preserved.
Only the Go/UI gateway was rebuilt and restarted.

## Delivered

- Native Go public-source downloader: HF and ModelScope search, explicit file
  selection with immutable identities, public HTTPS links, durable plans,
  measured byte progress and validated manual resume. Acquiring files does not
  load, convert or qualify a model. Gated/private repositories are not supported.
- Canonical local Chat/Pi conversations with import/export, revision conflicts,
  restart recovery and explicit transcript handoff. No automatic tool replay,
  reconnect or retry. The gateway saves Chat output even if its browser detaches.
- Protected local Pi source copies, captured independent build/test commands,
  receipts, version hashes, diff and explicit conflict-checked Apply. Direct
  mode is a separate deliberate choice. Actual session reasoning is displayed.
- Deletion removes owned conversation/Pi files and unshared attachments after
  clean close. Original projects, exports and external backups are outside that
  deletion. Filesystem removal does not guarantee forensic erasure on SSDs.

## Evidence and scope

| Check | Observed result |
|---|---|
| Full local Go suite | 174 top-level tests, race detector PASS; vet PASS |
| Node unit/protocol tests | 49 PASS |
| Firefox product fixture | 28 checks PASS, no real model |
| Firefox downloader fixture | 9 checks PASS, no real download |
| Deployed read-only browser | 10 checks PASS, actual UI/screenshots |
| Public source metadata through Go | HF and ModelScope PASS; no weight bodies acquired |
| Actual protected Pi integration | One small C sum repair, 15 checks PASS |

The real Pi task concluded naturally. The independently executed commands were
`gcc -std=c11 -Wall -Wextra -Werror main.c -o sum` and
`/bin/sh -c 'test "$(./sum)" = 42'`: both exited 0. Only `main.c` changed;
an existing untracked note and the original source remained intact before Apply.
Explicit Apply produced the exact verified source hash. Closing and deleting
the fixture conversation removed its canonical and owned native Pi files while
preserving the fixture project. The observed agent-plus-verification interval
was about 30 seconds, not a throughput benchmark or an intelligence score.

Negative fixtures include changed/missing resume validators, ignored byte ranges,
wrong hashes, insufficient disk reserve, path/symlink/hardlink escapes, active
conversation deletion, stale revision/write resurrection, interrupted streams,
quota exhaustion, modified originals/candidates and an agent claiming success
without independently passing tests. Setup errors in early fixtures were fixed
without relaxing production admission checks. Mocked and actual evidence remain
separate; hosted CI is not hardware qualification.

## Remaining boundaries

Protected copies are bounded text-source snapshots, not arbitrary repositories
or complete Git clones. Protected SSH and isolated remote verification are not
implemented. Test success proves the configured commands passed, not all possible
correctness; changed test definitions require separate review acknowledgement.
Automatic deletion or permission-only Apply is blocked. General reinstall/
bootstrap portability and multimodal model support remain separate work.

Existing tab-only chats must be exported **before reloading the old frontend**,
then explicitly imported. JSON exports do not bundle attachment binaries.

Read the [download contract](DOWNLOADS.md), [conversation lifecycle](CONVERSATIONS.md)
and [protected workspace guide](PROTECTED_WORKSPACES.md) for limits and API details.
