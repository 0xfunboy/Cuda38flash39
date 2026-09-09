# Pi workspaces

The workspace UI uses the actual upstream Pi coding agent, not the previous
product coding loop. Starting a session starts `pi --mode rpc`; it does not
submit a model request. Sending a prompt explicitly lets Pi read, edit, write
and run commands in the selected workspace. New local sessions default to a
protected, bounded source copy with independent checks and explicit verified
Apply. Direct mode must be deliberately selected and acknowledged. This is
separate from the older Advanced coding loop. Keep using a clean branch or
disposable worktree for reviewed changes.

[Protected snapshots, limits, receipts and Apply](docs/PROTECTED_WORKSPACES.md)
define the safety contract. Protected mode copies current dirty/untracked
eligible sources, not Git history or an unlimited full repository. The original
root is never reset/stashed. Remote protected mode is explicitly blocked.

## Pinned implementation

- Package: `@earendil-works/pi-coding-agent` **0.85.1**.
- Upstream commit: `d981de1229ef899957bbe968bc8dcda02a21f477`.
- Installed locally under `.tools/pi`; Node **22.22.1**, required Node >=22.19.0.
- Exact package, integrity and documentation links: [runtime/pi-pin.json](runtime/pi-pin.json).
- Installation uses the lockfile and `--ignore-scripts`; no global install,
  remote install, provider login, external inference service or model download.

Primary documentation: [Pi RPC protocol](https://github.com/earendil-works/pi/blob/d981de1229ef899957bbe968bc8dcda02a21f477/packages/coding-agent/docs/rpc.md),
[custom providers](https://github.com/earendil-works/pi/blob/d981de1229ef899957bbe968bc8dcda02a21f477/packages/coding-agent/docs/models.md),
[official SSH tool example](https://github.com/earendil-works/pi/blob/d981de1229ef899957bbe968bc8dcda02a21f477/packages/coding-agent/examples/extensions/ssh.ts).

The adapter uses Chat Completions with ordinary function tools, `max_tokens`,
`reasoning_effort`, streaming usage and system/user roles. `supportsStrictMode`
is false. Only low/high/max are selectable; unsupported levels are not silently
mapped. Compaction and automatic retries are disabled to avoid hidden extra
model requests or replay after an uncertain paired result. Provider compatibility
is a separate gateway qualification gate; the UI cannot enable it itself.

The reasoning value is captured when creating a workspace session. The Chat
context-window, response-limit and thinking-budget selectors do not configure
Pi and are not shown on Coding. Changing the sidebar does not update an
existing Pi process. Pi/tool requests still obey gateway/runtime admission and
request deadlines; this distinction is not a promise of unlimited output.

## Local session

1. Select a project directory within a configured `workspace_roots` entry.
   The broad root itself is not suitable if it contains live runtime, weights
   or product state. Such mounts are rejected. A project subdirectory is required.
2. Choose protected mode, reasoning and trusted build/test commands, then create
   the session with explicit confirmation. It becomes `CONNECTED`; original and
   working roots are separately reported. An optional canonical conversation
   links Chat and Pi without implicitly submitting its old transcript or tools.
3. Start Pi. This runs RPC startup/get_state only; state becomes `READY`.
4. Send a prompt only when `capabilities.prompt` is true. An accepted RPC
   response means admission, not completed work. Actual Pi events distinguish
   agent activity, tool execution and final responses.
5. Natural completion starts independent checking on a new snapshot using the
   preconfigured commands. Inspect `verification`, review the diff and explicitly
   apply only its exact `TEST_PASS` candidate. Agent self-reports do not qualify
   code. Changed test definitions require an additional acknowledgement.
6. Abort clears Pi's pending queue and asks Pi to become idle. Close drains an
   active generation before ending the owned process tree. It never stops ranks.

Pi runs in bubblewrap with separate filesystem, PID and **network** namespaces.
Only the protected working copy (or explicitly direct project) is writable, alongside private Pi configuration,
session records and temporary storage. Runtime and operating-system tools are
read-only. The sandbox does not mount the host home, SSH keys or systemd bus.
Project extensions, skills, templates and context-file discovery are disabled.

There is no shared host network: generated commands cannot address either
inference rank. An internal loopback listener forwards solely to a private Unix
socket implementing the session's limited model-generation capability. Pi sees
an ephemeral token for this capability, **not** the product's administrative
bearer token. Task, operation, lifecycle and workspace APIs are not exposed by
the socket. Ordinary package downloads inside the local shell are consequently
unavailable; install dependencies separately through an explicit trusted workflow.

Every Pi process tree is in an owned user-systemd scope:

| Property | Effective required value |
|---|---:|
| MemoryMax | 2 GiB |
| MemorySwapMax | 0 |
| TasksMax | 128 |
| CPUQuota | 200% |

Startup checks the actual scope's properties, InvocationID and cgroup path.
Close drains generation and then stops the entire owned scope, including
detached children, only if its unit name and InvocationID still match. A changed
owner is refused, not killed. Missing or already inactive scopes are harmless.
These bounds protect the running GLM from an accidental fork/allocator storm.
They can also prevent large project builds; that is a visible resource failure,
not a reason to silently remove the limits.

## SSH sessions

SSH currently supports **explicit direct mode only**, requiring
`mode:"direct", allow_direct:true`. Protected SSH snapshots, safe remote Apply
and independent fresh remote verification are not implemented. They fail
explicitly rather than silently granting direct writes. All remaining remote
permissions and caveats below still apply.

Save a host preset containing only name, explicit hostname/IP, port, user,
project root and optional private-key **path**. Key contents are neither read
into API responses nor persisted by the product. Presets reject arbitrary SSH
command fragments and unsafe option-like hostnames. Private-key files must be
regular owner-only files.

Authentication can use an existing SSH agent, a specified key, or a password
supplied only when pressing Connect. The password is passed once from a private
Unix socket to the bundled SSH_ASKPASS helper. It is never written to a preset,
file, command line, process environment or application log. It remains transient
process memory during authentication. Password creation through session metadata
is refused: supply it only to the connect endpoint.

Host verification uses `StrictHostKeyChecking=yes` and the existing
`/home/funboy/.ssh/known_hosts`. An unknown or changed key fails closed. Review
and install the correct key through a trusted out-of-band SSH workflow; the UI
does not offer a bypass. No remote software is automatically installed.

The connection owns one ControlMaster. Further requests use only that socket,
with direct connection fallback disabled. Pi stays local; its four remote
read/write/edit/bash tools delegate through the same confirmed SSH session.
The remote needs bash, realpath and ordinary Unix tools, not Pi or npm.

**Remote authorization differs from local sandboxing:** remote shell commands
run with the selected account's authority. The local scope does not limit the
remote machine's resources. Path checks protect ordinary tool paths from accidental
workspace escapes, but they are not a remote operating-system security sandbox.
Use a dedicated restricted account/container for untrusted remote work. Do not
select an account with control of production inference ranks for autonomous code.
Closing an SSH channel does not guarantee that deliberately detached remote
children have stopped. No remote-host functional qualification is implied by
the local protocol/unit tests.

## Files and terminal

The file browser lists directories and reads bounded UTF-8 text files. Local
reads use `os.OpenRoot`, reject traversal and cannot follow a symlink outside
the selected root. Hidden paths and binary files are not displayed. Text reads
are capped at 256 KiB; large listings indicate truncation.

Four read-only diagnostics are available without model inference: working
directory, Git status, listing and disk space. The custom command box requires
an idle, started Pi session and explicit confirmation of the command and root.
Local commands use Pi's actual RPC `bash`, inside the same network-isolated,
resource-limited sandbox. Bash output enters Pi's context on the next prompt;
running the command itself does not call GLM. Commands have a 60-second UI/API
budget and bounded output. SSH commands use the saved connection and carry the
remote-account caveats above. Commands cannot overlap a Pi generation.

## API contract

All endpoints require the normal product Bearer authentication. Every mutating
request requires `confirm:true`; unknown JSON fields are rejected.

| Endpoint | Purpose |
|---|---|
| `GET /v1/workspaces/options` | Installed Pi, capabilities, roots and auth methods |
| `GET/POST /v1/workspaces/presets` | Read/save secret-free host metadata |
| `GET/POST /v1/workspaces/sessions` | List/create sessions without inference |
| `GET /v1/workspaces/sessions/{id}` | State and capabilities |
| `POST .../{id}/connect` | Explicit SSH connection; transient optional password |
| `POST .../{id}/start` | Start upstream Pi RPC, no prompt |
| `POST .../{id}/prompt` | Send `{message,confirm:true}` when qualified and idle |
| `POST .../{id}/abort` | Clear queue and cooperatively abort |
| `POST .../{id}/close` | Drain and close only this owned workspace process tree |
| `GET .../{id}/events?after=N` | Actual ordered RPC events, bounded replay |
| `GET .../{id}/files?path=relative` | Directory listing |
| `GET .../{id}/file?path=relative` | Bounded text read |
| `POST .../{id}/terminal` | `{command_id,confirm:true}` or `{command,confirm:true}` |

State and events are stored under `state/workspaces/{id}`. RPC raw logs have a
16 MiB cap and an explicit truncation record; live replay retains at most 1000
events/8 MiB. Upstream Pi session data is kept separately in that private
session directory. On gateway restart, saved sessions become closed records;
they are never implicitly reconnected or restarted, and credentials are not
resurrected. Create a new session to reconnect. No generation is retried.

New sessions link to the same local canonical conversation used by Chat.
Explicit transcript handoff stages context for the next new Pi prompt; it is
not native process/KV restoration and never replays tools. Original role and
origin are retained. Explicit conversation deletion requires linked workspaces
to have closed cleanly and removes their owned private records, snapshots and
receipts; it does not delete project files or exported copies. History and a
live agent process are different resources.

## CPU-only verification

```sh
.tools/go/bin/go test ./... -run '^TestWorkspace' -count=1 -race
```

This includes real Pi startup/get_state/close, actual Pi bash, effective cgroup
limits, a blocked host TCP listener and model-discovery through the Unix socket.
It does not call GLM, connect to a real SSH host, download models or change
inference services. Fake-protocol tests are not a substitute for the separately
recorded real GLM read/edit/build/test task.
