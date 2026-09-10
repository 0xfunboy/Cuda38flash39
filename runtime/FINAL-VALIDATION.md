# Final native validation checklist

Historical acceptance procedure for the isolated native gateway. Since
SYSTEM-CLEANUP-001, the native backend is the production backend, the duplicate
`strixglm-native.service` is retired, and engine paths have moved. Use
[OPERATIONS.md](OPERATIONS.md) for current startup, maintenance and recovery;
do not execute this old port/layout procedure against the live pair unchanged.

This is a short acceptance procedure, not a benchmark sweep. It requires an
explicit maintenance window after active coding work has drained. Preserve the
original gateway configuration and use a **new** snapshot filename each time.
Do not send direct rank POSTs: only the paired coordinator may drive TP2.

## 1. Prepare without changing the running GLM

```sh
./.tools/go/bin/go test -race ./...
./.tools/go/bin/go vet ./...
./bin/strixglm cluster status
./bin/strixglm cluster verify
./bin/strixglm cluster snapshot-legacy /home/funboy/StrixHaloClusterGLM/state/native-validation-rollback.json
```

The configured launcher must already exist at the same path on NODE02. Startup
compares its small SHA-256 with NODE01. Snapshot the three original units'
nonces/InvocationIDs and the original raw/coder health. No GLM request occurs in
these commands. If the snapshot pathname already exists, choose a fresh name;
never overwrite a preserved rollback receipt.

## 2. Bring up the native pair once

```sh
./bin/strixglm cluster stop-legacy /home/funboy/StrixHaloClusterGLM/state/native-validation-rollback.json
./bin/strixglm cluster start
./bin/strixglm cluster --config config.native.json serve-pair
```

Run `serve-pair` in its own terminal/service. Read-only checks on its configured
loopback listener (default18094): `/health` must show both ranks true, busy false,
poison empty; `/v1/models` must name the unchanged GLM Flash. The owner must show
two native units with new nonces/InvocationIDs, TP2/PP1 and k5/local0 unchanged.
Keep rank log paths from `state/cluster/logs/` for this acceptance run.

Start the product gateway using a separate `config.native.json` whose backend
is the native listener. Keep all other qualified profile/model/sandbox settings
unchanged. The gateway must not hold `state/cluster/pair.lock` while forwarding
to the native backend; that backend already owns this admission lock. Never
run two native coordinators against the same pair.

The supplied native configuration uses gateway127.0.0.1:18095, backend18094 and
private state `state/native-gateway`; its admission lock remains the legacy
controller lock, not the native backend's own lock. It preserves the current
JSON context format. Profile selection is recorded in config.native.json;
profile aliases are not evidence of independently qualified reasoning modes.
`deploy/strixglm-pair.service` and `deploy/strixglm-native.service` are templates,
not installed/enabled by this document itself. Do not enable or switch them
silently. Their ordering directives do not start the model ranks or their peer
service automatically.

The paired service handles SIGTERM/SIGINT by closing new admission and waiting
for both active rank responses, including work detached by a disconnected
client. Its generation deadline comes from the app configuration's
`model_timeout` (currently600s), with10s for HTTP shutdown. The service manager
allows1850s to cover the supported1800s maximum plus cleanup. A shutdown timeout
persists poison; a late successful rank response cannot silently clear it.

## 3. Functional acceptance through the product, not old scripts

Use the existing frozen suite and product CLI/API:

- One standalone algorithm and one parser task.
- One compilation task and one multi-file repository bugfix, with independent
  tests and the existing repair loop. A task must finish naturally, not merely
  reach its token cap. Preserve task IDs and product receipts.
- One naturally exercised repair; if existing tasks pass first try, report that
  fact and use the dedicated deterministic repair regression, not a fake failure.
- Streaming `chat`, `/v1/models`, OpenAI `/v1/chat/completions`, coding API, and
  browser frontend. Verify SSE content and reasoning stream correctly and the
  final usage/metrics event plus `[DONE]` arrives only after the paired drain.
- Cancel one coding task. It may become draining before CANCELLED; generation
  admission must stay closed until both rank responses terminate. Confirm a
  subsequent task works without an isolated rank restart.
- Explicit apply on a disposable repository: inspect the patch, confirm apply,
  verify tests and executable bits; verify original source remains untouched
  without confirmation. Run sandbox isolation regressions locally.

For each model task preserve requested/effective reasoning, prompt/output
tokens, status, repairs, independent tests, wall time, TTFT and runtime-provided
decode/acceptance. Do not derive generation speed from GPU activity. Native
coordination forwards rank0 telemetry; timing/usage differences between ranks
are not generated-output divergence.

## 4. Whole-pair restart and rollback

After admission is idle, save the native owner IDs. Stop/drain the native
gateway and coordinator as described in OPERATIONS.md; neither is included in
the controller's owned rank set. Execute
`./bin/strixglm cluster restart`, and prove **both** InvocationIDs changed. The
Go coordinator must be newly started after any in-memory poison. Start the
coordinator then gateway, verify paired health, and repeat a
short product API smoke and both-rank health; save the new owner IDs.

Stop/drain both native HTTP services again before restoring the original whole
pair, not just one rank:

```sh
./bin/strixglm cluster stop
./bin/strixglm cluster restore-legacy /home/funboy/StrixHaloClusterGLM/state/native-validation-rollback.json
```

Return the product gateway to the preserved configuration. Check original raw
port18091, coder18092, original model/settings and all three retention records.
Keep the native configuration and results ready for an explicit adoption choice;
do not remove or overwrite the old fallback automatically.

If legacy is poisoned before the window, use the explicit `recover-legacy`
procedure in `OPERATIONS.md`, not ordinary stop and not manual marker deletion.
Its recovery journal and poison archive are part of the evidence. A native
deadline/mirror failure similarly requires both-rank recovery; an HTTP retry is
not recovery. No isolated rank command is authorized by this checklist.
