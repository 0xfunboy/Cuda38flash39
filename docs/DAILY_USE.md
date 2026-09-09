# Daily use

**HaloClu is useful now as a supervised coding assistant.** Use it to
explain code, draft changes, investigate failures and implement bounded tasks.
It is not qualified to make unattended production changes or to replace code
review, a compiler and independent tests.

## What the evidence supports

| Evidence | What it establishes | What it does not establish |
|---|---|---|
| Ten distinct compact C++ fixtures eventually passed; nine first attempt, one after repair | Useful bug-fix and feature work in the tested workflows | A general coding success rate; these were mostly small synthetic repositories |
| Actual local Pi read/write/bash task; unedited C passed 213 independent checks | The integrated agent delivered one bounded program that passed independent verification | 213 separate problems, broad Pi reliability, or an error-free self-report |
| Audited `low` and `max` chat answers contained reproducible C defects | Review and executable checks remain necessary | That either reasoning setting is always better |
| Preserved GLM benchmark: 24.85 decode / 23.67 HTTP tokens/s | Performance of the recorded 109-input / 256-output workload | The rate for every prompt, final-code throughput, or successful tasks per second |

See [qualification and preserved negatives](../QUALIFICATION.md) for the
workloads, measurements and differences between the historical coding workflow
and the newer Pi integration. A naturally concluded answer can still be wrong.

## A practical workflow

1. Start from a clean branch or disposable worktree. Select only the project
   directory, not a home directory containing credentials or the running model.
2. Give one bounded task with an explicit contract: files in scope, expected
   behavior, edge cases, and the existing build/test commands. Avoid pasting an
   entire large repository when Pi can inspect the relevant files.
3. Start with `low` reasoning for routine work. Try `high` or `max` for a specific
   difficult problem, not as a promise of better answers. The measured small
   comparison favored `low`; that is not a universal model ranking.
4. Review the actual diff. Compile, run independent tests and check boundaries,
   invalid inputs, error handling and resource use. For C/C++, enable warnings
   and applicable sanitizers. A model saying “tests passed” is not a test log.
5. Commit only after checking the result. Keep generated changes away from
   production until the ordinary project review and release process passes.

**Pi changes files directly.** Sending a prompt authorizes its available tools
within the selected session. It does not stage every change behind the legacy
coding workflow's Apply button. Local sandboxing limits access and resources;
it does not make an incorrect change correct.

## Limits that matter during work

- Chat's output budget includes reasoning as well as the final answer. A cap hit
  means **INCOMPLETE**, not PASS. Increasing the cap can help delivery but cannot
  prove correctness, and the backend also has a request deadline.
- The context selector is a capacity setting, not a quality certificate.
  Historical cross-file tasks failed at larger contexts; near-32K actual input
  was incomplete. Those results used an older source-packaging workflow, not a
  completed long-context qualification of Pi. Prefer focused file inspection.
- Local Pi has no general network access and a 2 GiB memory limit. Install
  trusted project dependencies separately; large builds may exceed the sandbox.
- Tool turns are buffered until the paired result agrees. Ordinary chat streams;
  the tool UI must not imply that a buffered result had live token delivery.
- Pi uses the reasoning selected when its session is created. Chat's context,
  response-limit and thinking-budget controls are not passed into Pi and are
  hidden on Coding. Runtime limits still apply; moving a sidebar selector does
  not reconfigure an existing Pi process or promise an unlimited response.
- Remote SSH commands have the connected account's privileges, not the local
  sandbox's restrictions. Real-host login and remote coding are not yet
  qualified on the user's selected host. Use a restricted account, not an
  account controlling the inference ranks.
- PDF support reads its text layer, not scanned pages or visual diagrams.
  Binary attachments expose limited hex/strings, not general binary analysis.
- Export important chat conversations before reloading. Pi stores session
  records, but a gateway restart does not automatically resume an agent or SSH
  connection. Never publish tokens, private keys, session logs or uploaded files.

Exact isolation, permissions and lifecycle behavior are documented in
[Pi workspaces](../WORKSPACES.md).

## Shared options

Open **Options** in the sidebar to select the interface language and display
preferences, connect with the gateway token, or manage new API admission.
These settings are shared across pages; reasoning, context and output budgets
remain generation controls, not interface preferences. Generation is hidden
on Models, Benchmark, Cluster and Options. Only the relevant reasoning control
appears for Pi; the complete Chat controls remain on Chat.

Advanced / legacy is hidden by default. Its Options checkbox can reveal the
older snapshot/build/test/confirmed-Apply workflow when needed. Hiding that menu
entry does not disable its API or cancel existing work.

Only non-secret display preferences are stored in the browser. The connection
token and SSH passwords stay in tab memory. Explicit token rotation invalidates
the old gateway credential for new requests: reconnect other browser tabs, Pi
clients and API integrations with the new token. It neither rotates SSH keys
nor changes the paired model runtime. API switches pause new work; they are
not an emergency cancellation or a firewall. [Exact scope](OPTIONS.md).

## Useful next checks — not a prerequisite to starting

These are proposed follow-ups, not tests already performed or an automatic
benchmark campaign. Use a disposable branch and preserve failed attempts.

| Priority | Bounded check | Acceptance evidence |
|---|---|---|
| 1 | Three real-project tasks: one bug fix, one feature, one refactor | Independent tests, reviewed diffs, natural completion, elapsed time and all attempts recorded separately |
| 2 | One task on a chosen restricted SSH host | Trusted host key, actual login, intended file changes only, tests, and connection cleanup |
| 3 | One interrupted session in a disposable project | Clear partial-change state, safe generation drain, no automatic replay, and healthy paired inference afterward |

These checks increase confidence in the way **you** use the product. They do
not require another model, a quantization change, a parameter sweep or a new
cluster research campaign.
