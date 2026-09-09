# Protected Pi workspaces and independent verification

New local browser/API sessions default to **protected** mode. Pi edits a private
bounded copy of the selected project's current source files, not the original
directory. This is the upstream Pi agent with a different working directory,
not a replacement agent loop. The original and working roots are reported
separately; changing or closing a session never stashes, resets or checks out
the user's repository.

## Snapshot scope

The copy captures current eligible UTF-8 source files, including dirty tracked
files and ordinary untracked files. It does **not** reconstruct HEAD or discard
local changes. Source permissions and original bytes are retained for review
and conflict checks. Recognized sensitive paths, hidden files/directories
(including `.git` and `.env`), generated/dependency trees such as `node_modules`,
`build`, `dist`, `target`, `vendor`, and runtime/state/weight paths are excluded.
Binary files are not a supported editable snapshot format. The exclusion list
is reported; excluded dependencies may make a build unavailable. A project with
secrets in ordinary source code is not automatically safe to submit.

Limits use the existing configured source quotas: the reference deployment
allows **300 files, 256 KiB per file and 32 MiB total source**. Traversal is
bounded too. Symlinks, hardlinks, oversized source files and quota violations
fail closed; choose a smaller project or explicitly inspect the issue. This is
not a full arbitrary-size Git worktree clone and does not provide Git history
inside the private working directory. No model weights or new dependencies are
downloaded to create a protected session.

## Direct mode and SSH

Direct mode requires both `mode:"direct"` and `allow_direct:true`, with an
explicit UI warning. Pi tools then write to the selected original directory;
there is no deferred Apply or automatic rollback. Independent local checks can
still run against a separate fresh copy, but they cannot undo direct edits.

**Protected SSH is blocked** with `BLOCKED_REMOTE_PROTECTION`. There is no silent
fallback to direct mode, no remote snapshot framework and no claim that SSH
commands are sandboxed. A remote session must explicitly select direct mode;
the selected SSH account determines its authority. Independent fresh remote
verification is also unavailable and reports `UNVERIFIED`, not a model-derived
PASS. Local transport tests do not qualify real-host remote coding.

## Commands are selected before Pi starts

Supply your trusted `build_command` and `test_command` when creating the
session. They use the existing command syntax: an argument array or a string
parsed into arguments. Shell operators require an explicit shell, for example
`["/bin/sh","-c","./program > actual.txt && diff -u expected.txt actual.txt"]`.
Commands are frozen for the session; Pi's answer cannot replace them.

After a naturally concluded `agent_end`, the gateway automatically runs those
commands against a **fresh private snapshot** of the agent's candidate source.
The snapshot used for checking is separate from Pi's working tree and the
original project. It reuses the existing network-isolated sandbox, with at most
1 GiB memory, 64 tasks, no swap and 90 seconds per command. Build artifacts stay
inside that disposable verification snapshot. Test execution is trusted user
authority over untrusted candidate code, not host-shell execution.

The product allows at most **one active Pi scope** plus one independent
verification sandbox. Pi retains its existing 2 GiB / 128-task / 200%-CPU limits.
Close the prior Pi session before starting another. An exited launcher does not
release that reservation until the owned process scope has been closed.

## Status is evidence, not the model's self-report

| Status | Meaning |
|---|---|
| `UNVERIFIED` | No configured test, unavailable verification setup, or changed candidate; no verified Apply |
| `VERIFYING` | Independent command execution is in progress; new prompts/Apply are unavailable |
| `BUILD_PASS` | The configured build exited successfully, but no test command was configured; no verified Apply |
| `TEST_PASS` | The configured commands completed with passing exit codes on the recorded source snapshot |
| `FAIL` | A configured build or test failed; output and exit code are retained |
| `INCOMPLETE` | The agent did not conclude naturally, or a check timed out; not a completed solution |

An agent saying “all tests passed” does not set any passing status. Natural
completion alone does not prove correctness. `TEST_PASS` means those user
commands passed, not that every requirement, edge case or security property is
proven. A manual Verify can check a candidate without another model request;
it does not turn a previously incomplete agent response into natural completion.

Receipts record original and candidate file SHA-256 values, a candidate-tree
digest, exact commands, real exit codes, bounded output, elapsed time, completion
status and excluded files. Receipt/artifact paths remain private under
`state/workspaces/<id>/verification/<verification-id>/`. Commands are executed
independently; the test design is not automatically independent of the agent.
Heuristically identified test/spec/fixture/build-definition edits are therefore
listed prominently. Such edits may weaken the test suite; passing modified
tests is **not** reported as passing an unchanged hidden oracle.

## Review and explicit Apply

Review the candidate diff and verification receipt before applying anything.
Only a current, unapplied protected-local `TEST_PASS` candidate can apply.
When test/build definitions changed, a separate acknowledgement requires
`allow_test_changes:true`; inspect those changes rather than accepting the
agent's claim. The heuristic is a warning aid, not perfect test-file detection.

Before Apply, the gateway checks the original directory identity, the complete
eligible original source snapshot and current candidate hashes. It then reuses
the existing verified apply transaction: all affected originals and pinned
verified outputs are checked, original bytes/modes are backed up, and each file
is rechecked before its atomic replacement. Conflicts refuse the operation;
the code does not reset or overwrite changed originals to force success.
This is conflict-checked file application, not a filesystem-wide atomic commit
against arbitrary external writers. Avoid editing the same project concurrently.

Apply currently supports eligible source modifications and additions. **File
deletion and permission-change application are blocked**, not silently ignored
or partially applied. Review such changes manually or use a fresh explicit
workflow. Backups and transaction receipts are retained for recovery until
explicit deletion of their owning conversation/session records; exported
copies and original project files are not deleted by that action.

A new Pi prompt or custom shell command invalidates prior verification.
Review/Apply rechecks candidate hashes, including edits made outside the UI.
Checks cannot authorize a different file version. After a gateway restart,
saved sessions are closed records: their diff remains reviewable, but they are
not adopted, resumed or applicable automatically.

## API additions

Session creation accepts these fields in addition to the existing local/SSH,
reasoning and conversation identity:

```json
{
  "kind": "local",
  "root": "/allowed/project",
  "mode": "protected",
  "build_command": ["gcc", "-Wall", "-Wextra", "-Werror", "main.c", "-o", "main"],
  "test_command": ["/bin/sh", "-c", "test \"$(./main)\" = 42"],
  "confirm": true
}
```

| Endpoint | Action |
|---|---|
| `GET /v1/workspaces/sessions/{id}/review` | Bounded source diff, candidate hashes, exclusions and verification |
| `POST .../{id}/verify` with `{"confirm":true}` | Start asynchronous independent checking; no model generation |
| `POST .../{id}/apply` with `{"confirm":true}` | Apply only the exact eligible verified candidate |
| `POST .../{id}/apply` with `{"confirm":true,"allow_test_changes":true}` | Same checks, plus explicit acknowledgement of modified test/build definitions |

All endpoints require gateway authentication. Workspace API admission controls
also cover Verify and Apply; status/inspection remain available. Nothing in
these actions restarts an inference rank or changes the GLM engine.
