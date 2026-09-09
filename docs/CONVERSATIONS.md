# Persistent Chat and Pi conversations

Chat and Coding share one server-generated conversation ID. The canonical
transcript is stored locally in
`state/conversations/<id>/conversation.json`, not in browser local storage.
The directory is private; records and atomic-write files use owner-only
permissions. Credentials still remain outside browser persistent storage.

## Recovery and interchange

Reload, reconnect with the gateway token and select a conversation. Chat
messages, recorded reasoning, status and settings remain available. Pi final
messages and tool results are linked to the same history; existing Pi event
logs are also readable after a gateway restart. Recovery does not reconnect
SSH, start Pi, retry a model request or repeat a tool command.

**Continue in Coding / Pi** selects the shared history. Create a session linked
to it, then explicitly **Stage shared history** and submit a new instruction.
The pinned Pi RPC has no generic importer for arbitrary Chat tool history, so
HaloClu transfers a quoted transcript as user context on the next prompt. This
is not exact native session, tool-state or KV-cache restoration. The handoff
limit is 128 KiB of transcript, with no silent truncation.

**Open in Chat** displays the shared Pi conversation. Model replay includes
user messages and completed assistant text only. Tool calls/results remain
inspectable history, not fabricated tool-role messages or commands to replay.
Reasoning and incomplete/error replies are retained for review but are not
silently promoted to completed answers in replay.

Older Pi sessions without a shared ID require explicit read-only recovery.
HaloClu imports recorded `message_end` events and links their existing private
session directory. A capped or malformed raw log is not presented as a complete
recovery. Original records remain available when recovery is refused.

## Streaming and recorded measurements

The gateway, not the browser, saves Chat output. Progress is saved periodically
while a request runs; the drained final result is saved even if the browser
disconnects. A gateway process restart marks previously unfinished replies
`interrupted`; it does not leave a permanently live spinner or retry work.
The latest unsaved progress immediately before a crash cannot be guaranteed.

Each saved assistant message retains effective settings and available backend
metrics under `settings.metrics`, including measured TTFT fields when supplied.
Gateway-observed and server-reported timing remain distinct. A metric absent
from the request/result remains unavailable; history does not reconstruct a
fake live rate or turn browser render time into model throughput. Pi history
does not imply that every tool turn exposes the same metrics as plain Chat.

Only one new Chat/Pi prompt at a time may mutate the same canonical
conversation. This protects ordering; it does not change the model's paired
inference protocol or the existing global admission gate.

## Delete both views and their owned files

Deletion requires confirmation and the current revision. Close linked Pi
sessions and let active generation drain first; otherwise deletion returns
`409` with `needs_close`. Closing uses the existing whole-owned-scope lifecycle,
not an isolated process or inference-rank restart.

A successful deletion removes:

- The canonical conversation directory, including interrupted atomic-write
  copies inside that exact directory.
- Linked closed Pi session directories: native Pi sessions, event logs,
  configuration and other owned session artifacts.
- Referenced attachment uploads and extracted text, unless another retained
  canonical conversation still references them.

It does not delete the original project, model weights, user-downloaded JSON
exports or external backups. A shared attachment remains until its last owning
conversation is deleted. Direct attachment deletion returns `409` while a
conversation references it. No transcript-bearing tombstone is retained.

Deleted IDs cannot be recreated by delayed saves: updates require an existing
record, and new conversations always receive a new server-generated ID. This is
filesystem deletion, **not guaranteed forensic erasure** on SSDs or deletion
of copies made outside HaloClu.

## Retention, limits and import

Records remain until explicit deletion; there is no hidden automatic expiry.
Limits are 256 conversations, 8 MiB per canonical record, 256 MiB aggregate
canonical records and 4,096 messages per conversation. Existing attachment,
event-log and workspace bounds are separate. A quota error is not a successful
save and must not be confused with a model failure.

JSON export saves a user-controlled external copy. Import is explicit, creates
a new conversation, and does not invoke a model or tool. Exported attachment
IDs are not portable file copies; a new installation must upload the files
again. Imported transcript data is not evidence that a tool or test was
actually executed in the new session.

## API contract

All routes require the gateway bearer token.

| Route | Behavior |
|---|---|
| `GET /v1/conversations` | Metadata list under `conversations`, retention and limits |
| `POST /v1/conversations` | Create from `{title}`; server assigns ID |
| `GET /v1/conversations/{id}` | Canonical record plus safe `replay_messages` |
| `PUT /v1/conversations/{id}` | `{revision,title,messages}`; revision-checked update, no active generation or forged/edited Pi records |
| `DELETE /v1/conversations/{id}` | `{revision,confirm:true}`; delete owned local records from both views |
| `POST /v1/conversations/{id}/handoff` | `{workspace_id,revision,confirm:true}`; stage context, execute nothing |
| `POST /v1/workspaces/sessions/{id}/recover-conversation` | `{confirm:true}`; explicit legacy read-only import |

Chat requests opt into server persistence with `conversation_id`; this product
field is removed before forwarding to the model. Workspace creation accepts
the same ID, or creates one when omitted. Stale revisions return a conflict,
not a silent last-writer-wins overwrite.
