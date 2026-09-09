# HaloClu Options

The **Options** sidebar page holds controls shared by Chat, Coding, Models,
Benchmark and Cluster. It is separate from generation settings. Generation is
hidden on the four non-generation pages; Chat exposes its request settings,
and Coding exposes only the reasoning used when creating a Pi session.
Changing the display does not change model computation.

![Options on the actual reference deployment, with the credential field empty](assets/options.png)

## Interface and connection

English is the default; Italian is optional. Interface controls include
conversation text size (14/16/18), comfortable or compact density, automatic
thinking expansion, sidebar collapse and optional legacy navigation. Only this non-secret preference
whitelist is saved as `haloclu.preferences` in browser storage. An existing
`strixglm.language` preference is migrated; credentials are not migrated or
persisted. These are not model settings and do not carry a quality or
performance claim.

**Advanced / legacy is hidden by default.** Enable the interface option to
reveal the older isolated-snapshot coding workflow. This preference controls
navigation only: it does not disable legacy API admission, cancel tasks, delete
results or remove the code and regression tests. Use the separate legacy API
switch below if the intent is to pause new legacy work. The normal **Coding**
page remains the upstream Pi integration.

Enter the local gateway token to connect. The token remains in the current
tab's memory, not browser persistent storage. Reloading requires reconnecting.
Forgetting the browser credential signs this tab out; it does not revoke the
server token or cancel work already admitted. SSH passwords likewise remain
memory-only and are supplied only when connecting a workspace.

## Rotate the gateway token

Token rotation is an explicit authenticated action requiring confirmation.
It generates a new gateway credential and stores it in `state/api-token` with
mode `0600`. The old token stops authorizing new requests. Other tabs and API
clients must reconnect using the new token; already admitted work is not
silently cancelled.

This controls only the HaloClu gateway bearer credential. It is not a generic
secret vault and does not rotate SSH credentials, Hugging Face credentials,
model-runtime credentials or inference-rank ownership. Existing SSH host
presets and key paths retain their own controls. Never paste a real token into
an issue, screenshot or committed configuration.

## API admission switches

Authenticated `GET /v1/settings` reports the current configuration.
`PUT /v1/settings` saves explicit Boolean switches for all four API groups:

```json
{
  "api": {
    "chat": true,
    "workspaces": true,
    "legacy_coding": true,
    "operations": true
  }
}
```

The switches persist privately in `state/api-settings.json`.

| Switch | New work that can be paused | What remains available |
|---|---|---|
| `chat` | Public chat requests and attachment uploads | Work already admitted; an existing Pi turn is not interrupted |
| `workspaces` | Workspace create/connect/start/prompt/terminal actions | Reads, saved presets, abort and close |
| `legacy_coding` | New legacy tasks and patch apply | Existing task inspection and safe cancellation |
| `operations` | New benchmark/download operations | Existing operation status and cancellation |

These switches are admission controls, not cancellation, credential isolation
or a firewall. They do not revoke an already running agent or its workspace
authority. Use the relevant abort/cancel/close control when the intent is to
stop existing work. Administrative Options remain reachable to restore API
admission.

Token rotation uses authenticated `POST /v1/settings/token` with
`{"confirm":true}`. Do not put credentials in URLs or shell history.

## Listener and backend configuration

The current listen address and backend are informational. Editing them requires
an explicit deployment configuration change and a controlled gateway restart;
the browser does not change them or expose a new public listener. Keep the
gateway private and use an SSH tunnel for remote browser access. Restarting
the gateway does not authorize restarting one model rank.

Neither API admission changes nor language/display preferences alter the
qualified GLM target, weights, TP2/DFlash2 settings or paired correctness checks.
