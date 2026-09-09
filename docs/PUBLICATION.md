# Publication and local-data boundary

This source repository documents one reference two-node Strix Halo deployment.
It is not a dump of the machines or of their inference state. Paths, host
aliases and private-network addresses in recipes are deployment references,
not portable defaults or credentials. A fresh installation must adapt its
configuration before using lifecycle commands.

## Initial publication review

A bounded read-only review on 2026-09-09 inspected the existing single-commit
history (`94f14bbbbaadcd1c4dccab281ac2449c4e8d14fd`) and the prospective source
working tree before publication documentation was added: 66 unique historical
blobs and 91 prospective text files, totaling 1,050,072 bytes. There were no
binary or model artifacts and no individual prospective file larger than 1 MiB.
Later presentation files are additional to these initial audit counts.

The scan checked private-key headers, common GitHub/Hugging Face/cloud token
formats, credential-bearing URLs, JWT-like values, long secret assignments,
and exact occurrence of the active product bearer token without printing it.
No actual credential was found in the scanned source or history. Two kinds of
matches were explicit negative-test fixtures: a fake credential URL and a
string labeled as a non-secret fixture token. This is a bounded source review,
not a guarantee that any arbitrary future addition is safe to publish.

Historical task identifiers and sample ownership/invocation identifiers are
test or deployment evidence, not login credentials. The configured home paths,
SSH alias and private-network addresses intentionally describe the reference
hardware. They must not be mistaken for a working installation on another host.

## Material that remains private

- `state/`: bearer token, conversation/session state, uploads, saved host
  configuration, runtime ownership, rollback snapshots, and generated receipts.
- `.tools/`: downloaded toolchains, Pi installation and dependency trees.
- `bin/`: compiled local gateway and rollback executables.
- `config.local.json`: local override configuration, when present.
- External model/framework installations and the original raw report tree.

These directories are excluded from version control. Existing private files
were not deleted or moved to publish this source. User conversation excerpts,
uploaded documents, passwords and private keys are not publication artifacts.
Source references to a raw report path do not make that report publicly
available; public summaries must be read with their stated scope and negatives.

## Before a future push

Review both staged names and the staged diff. Do not use force-add on excluded
state, uploads or downloaded assets. Check new fixtures for real credentials,
host exports, transcripts and identifying content. If a credential is ever
committed, adding an ignore rule later does not remove it from history: revoke
the exposed credential and assess the affected history before publishing.

Third-party provenance and component notices are in
[THIRD_PARTY_NOTICES.md](../THIRD_PARTY_NOTICES.md). The original project-code
license is separate from third-party terms; do not infer an open-source license
from repository visibility alone.
