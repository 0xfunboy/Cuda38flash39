# Exact whole-pair operations

Run from the product repository. These commands never invoke the old Python
controller. The existing Python CIRU inference engine is an external dependency.
`runtime/cluster.json` defines the paths, private rank ports and new frontend.

Run commands from /home/funboy/StrixHaloClusterGLM: the cluster configuration
path is resolved from the current directory. The CLI --config flag selects the
application configuration (including the paired request timeout); it does not
select a different runtime/cluster.json.

## Read-only checks and preservation

```sh
./bin/strixglm cluster status
./bin/strixglm cluster verify
./bin/strixglm cluster snapshot-legacy /home/funboy/StrixHaloClusterGLM/state/rollback-original.json
```

The snapshot destination must be a **new JSON file**, not a directory. The
snapshot holds the owner, qualified preset, exact systemd argv (including empty
arguments), properties and old retention identity. Runtime verification hashes
the small pinned code/native files and checks shard lengths; it never hashes or
copies all weights. Keep the snapshot private. No service is stopped here.

Use the identical absolute snapshot pathname for stop/recover/restore. Restore
checks that pathname against the stopped owner, not only file contents. After
a successful restore, InvocationIDs and retention records have changed: create
a new snapshot for the next maintenance window rather than replaying the old
one. This is a restorer for the pinned qualified recipe, not a generic importer
of arbitrary systemd properties or modified engine installations.

Restore also requires the stopped owner's epoch and complete rank/name/nonce/
InvocationID set to match the snapshot. It checks them on entry and re-reads
them after the slow integrity checks, before the first reservation or launch.
A replaced owner is left untouched; a matching path alone cannot authorize
restoration.

Install only the small product launcher at its configured path on NODE02 before
the first native launch; both copies must have equal SHA-256. Do not copy the
legacy repository, Python environment, model weights or reports.

## Explicit final-validation window, not automatic migration

Finish/cancel coding work and wait for the existing generation to drain. No
concurrent benchmark or raw API client may submit work during this window.

```sh
./bin/strixglm cluster stop-legacy /home/funboy/StrixHaloClusterGLM/state/rollback-original.json
./bin/strixglm cluster start
./bin/strixglm cluster --config config.native.json serve-pair
```

The last command serves the native paired API on the configured loopback port
(default18094). Set a separate product gateway configuration's backend to that
URL for the validation. Do not overwrite the fallback configuration. Native
paired admission uses `state/cluster/pair.lock`; the gateway must **not** hold
that same lock while calling it. Keep gateway admission serialized.

On any launch/load failure, read the durable owner. Cleanup uncertainty means
inspect both owned units and their nonces/InvocationIDs; do not use `pkill`, stop
a single rank manually, or remove poison while either old rank may be live.

## Restart and original rollback

First close native API admission. If the supplied user-service templates were
explicitly installed for this window, stop strixglm-native.service and wait for
completion, then stop strixglm-pair.service and wait for its paired drain. For
terminal-launched services send SIGTERM to their identified processes and wait
for completion. Do not use a broad process-name kill.

These HTTP services are not ranks. The controller owns only strixglm-rank0/1;
cluster stop/restart does not terminate a still-running native coordinator or
gateway. The service templates have Restart=no and no dependency that starts or
stops the ranks automatically.

```sh
./bin/strixglm cluster restart
```

For continued native use, wait for ready, then start the coordinator and gateway
in that order and check paired health before opening admission. For original
rollback instead, with native HTTP services still stopped:

```sh
./bin/strixglm cluster stop
./bin/strixglm cluster restore-legacy /home/funboy/StrixHaloClusterGLM/state/rollback-original.json
```

Restart never offers a rank parameter. Original restore requires the new owned
pair proven stopped and the preserved old owner still belonging to this
snapshot. It verifies old frontend/launcher/runtime identities, launches both old
ranks, waits for both health checks, then restores the original frontend18091.
It updates owner and retention InvocationIDs. The coder18092 is not removed.
The coder may remain running during native validation, but its legacy18091
backend is unavailable until rollback completes; preserved does not mean both
complete model pairs can run simultaneously. Start/re-enable only the gateway
configured for legacy18091 after original paired health returns.

After a native paired frontend has poisoned in memory, its process must also be
restarted after whole-pair recovery. Never clear a marker merely to make health
green. Final adoption is an explicit user choice; leave the fallback intact.

For the 2026-09-08 final-validation window the actual preserved snapshot is
/home/funboy/StrixHaloClusterGLM/state/native-final-20260908.json. Substitute that
exact path for the example rollback-original.json when restoring this window.
Do not create another snapshot from the already stopped legacy units to replace
the original receipt.

If the old frontend is already poisoned, ordinary `stop-legacy` deliberately
refuses it. The explicit recovery sequence is `snapshot-legacy NEW.json`,
`recover-legacy NEW.json`, then `restore-legacy NEW.json`. Recovery retains raw
poison in a durable intent, checks the complete original owner, stops all three
owned process groups, and only then archives the live marker. A foreign or
replaced peer causes zero stops and leaves poison intact. This is not an
automatic HTTP retry and never clears poison while a rank may still be live.

## Controller self-tests

`go test -race ./...` covers owner replacement, both-rank stop, locking,
identical paired input bytes, output agreement, SSE terminal withholding,
truncation/poison, and cancellation with detached draining. The harmless local
systemd probe used during development confirmed same-name transient restart
works even with an infinity-retention drop-in; its temporary unit/file were
removed. It did not touch either GLM rank.
