# Native model downloads

HaloClu includes a Go downloader in **Models → Download models**. It is an
acquisition tool, not an inference runtime or an automatic model installer.
The gateway does not load downloaded models, convert weights, extract archives,
execute repository code, change drivers or switch either Strix Halo rank.

**Find & download** is always above **Local & tested**, with section links and a
shared **Refresh Models** action. Both require HaloClu authentication, including
when browsing anonymous upstream sources. Public source access does not mean an
unauthenticated browser may use the server's disk or network. Before sign-in,
controls are locked; sign-out clears displayed inventory, paths and job details.
The two areas load independently, so a catalog error does not hide the downloader.

## Workflow

1. Connect with the gateway's local API token.
2. Search Hugging Face, ModelScope, or both. Inspect a repository to fetch its
   file metadata; choose the actual files needed for the intended runtime and
   quantization. Nothing is selected or downloaded automatically.
3. Create a plan. Review the files, total bytes, revision identities and managed
   destination in the preserved job.
4. Choose **Start** and confirm. One background acquisition job runs at a time;
   files within that job transfer sequentially. The browser displays received
   bytes, transfer rate and an ETA when there is enough measured data.
5. **Pause** or **Cancel** preserves partial files. **Resume** explicitly resumes
   the selected job. Closing the browser does not stop a server-owned transfer.
   Gateway interruption recovers an active receipt as `PAUSED`, never auto-starts.

The UI polls only when Models is visible and a recorded job is active. It does
not consume inference requests. Disk status and job inspection do not hash
existing model files again.

## Sources and formats

| Source | Discovery | Identity and verification |
| --- | --- | --- |
| Hugging Face | Anonymous repository search and file listing | Repository revision resolved to a 40-character commit. LFS SHA256 retained where returned; ordinary Git files may have size only. |
| ModelScope (`modelscope.cn`) | Anonymous OpenAPI search and legacy file listing | Each file is pinned to its returned commit and SHA256. This is a per-file manifest, **not a claim of an atomic whole-repository snapshot**. |
| Public HTTPS file URL | Explicit URL plus a HEAD metadata request | Known Content-Length is required. Optional supplied SHA256 is verified. Strong ETag/Last-Modified support safe resumption where available. |

Hugging Face `blob` and `resolve` file URLs are resolved through repository
metadata and normalized to immutable download URLs. Repository-page URLs return
a file-selection instruction; they are not saved as model weights. Direct HTML
pages are rejected.

GGUF, safetensors, tokenizer/config files and other regular file formats are
supported as opaque bytes. A VLM is a vision-language model category. vLLM is an
inference runtime. Neither is a hosting source. File availability and successful
download do not demonstrate model/runtime compatibility, image-input support,
distributed inference or retained intelligence. Select all required shards and
the appropriate sidecars only after inspecting the intended runtime's requirements.

Public repositories only: no Hugging Face/ModelScope login, gated access, private
repository token, cookies or ambient authentication is used. Upstream outages
and rate limits are reported. Combined search retains successful-source results
and names any failed source in `warnings`.

## Resume, integrity and recovery

Default destination: `<state_dir>/model-downloads/<job-id>/data/`. The destination
is server-controlled and private; callers cannot choose arbitrary filesystem
paths. Job receipts are stored beside `data` in `job.json` and are ignored by Git.

- Plans select 1–128 files, up to 8 TiB in total. A maximum of 200 receipts is
  retained by the manager. There is no implicit download-all or automatic cleanup.
- Before starting, the remaining selected bytes plus a **16 GiB reserve** must
  fit. Reserve is checked during acquisition as well. This does not reserve disk
  space against other processes.
- Creating the small durable plan itself requires the reserve plus 512 KiB of
  receipt headroom; planning does not preallocate model storage.
- Partial data has a `.part` suffix. HTTP resumption requires `206` with an exact
  matching `Content-Range`, expected total size and consistent stored validators.
  A server that returns `200`, `416`, a changed validator or an inconsistent range
  cannot overwrite the existing partial. A complete partial is checked locally,
  avoiding an unnecessary suffix request/`416`.
- Without ETag, Last-Modified or a pinned SHA256, an interrupted nonempty file
  cannot safely resume. The job reports that limitation; it does not silently
  restart from zero or merge unverifiable versions.
- After transfer, size is checked and SHA256 is computed if a source/user hash
  exists. `SIZE_AND_SHA256_VERIFIED` and `SIZE_VERIFIED_NO_SOURCE_SHA256` are
  deliberately different receipts. No absent checksum is described as verified.
- Atomic hard-link publication never replaces a final filename. The verified
  `.part` is retained as a recovery alias of the same inode, without a second
  allocation. **Do not edit a completed `.part`: it is the same file as the final
  filename.** The downloader never resumes a completed file.
- A hash failure preserves the partial. Resume rechecks it; this does not fix bad
  bytes. Inspect it manually or create a fresh plan. No automatic deletion occurs.
- Incomplete data with unexpected extra hard links, symlinks, path traversal and
  unrecorded final files are refused. Filesystem operations are constrained by
  Go `os.Root` and no-follow regular-file checks.
- Receipts survive normal restarts. Crash recovery stats partial data and requires
  explicit resume; it does not launch downloads or rehash unchanged completed
  weights. Previously recorded checksum receipts are historical verification,
  not a continuous integrity monitor against later local edits.

Transfer stalls cancel after 90 seconds without a body chunk. Network errors do
not trigger an application-level retry loop. Temporary CDN redirect URLs remain
only in memory; they are not saved in receipts or surfaced through transport
error messages. Signed/token-bearing **input** query URLs are refused.

## API

All routes require the existing gateway bearer token. It is never forwarded to
the download source. The **Benchmarks and download operations** switch in Options
gates plan creation and `start`/`resume`; read-only status and `pause`/`cancel`
remain available for cleanup.

| Route | Purpose |
| --- | --- |
| `GET /v1/downloads/options` | Sources, managed root, reserve and concurrency |
| `GET /v1/downloads/search?source=all&q=Qwen` | Search metadata; per-source warnings |
| `GET /v1/downloads/files?source=huggingface&repo=org/model&revision=main` | File listing and resolved identities |
| `POST /v1/downloads/plan` | `{source,repo,revision,files:[path]}` or `{url,sha256?}`; no transfer |
| `GET /v1/downloads/jobs` | Persisted jobs and latest progress |
| `GET /v1/downloads/jobs/{id}` | One job |
| `POST /v1/downloads/jobs/{id}/{start,resume,pause,cancel}` | Explicit `{ "confirm": true }` |

The downloader's separate HTTP transport disables proxies and cookies, checks
HTTPS/port 443 on initial requests and every redirect, rejects private/special-use
addresses, validates all DNS results, and dials a validated IP directly. This
prevents user URLs or DNS rebinding from reaching loopback, cloud metadata or
the paired inference rank endpoints. The implementation does not accept custom
request headers or a client-selected transport. Only test code injects an HTTP
fixture transport.

## Relationship to the earlier scripts

This is a native Go reimplementation of the preserved Python/curl acquisition
workflow in `huggingface-download-resume`: explicit selection, identity pins,
size/hash checks, preserved partials and no-overwrite publication. It does not
run or import those scripts. No Python, curl, aria2, package install or additional
server framework is needed.

The original scripts and manifests remain unchanged. Their existing destinations,
rank assignment and aria2 segment maps are **not imported** into these jobs.
A sparse aria2 partial is not a contiguous prefix and cannot safely be treated
as one. Resume old script-managed downloads with their original engine/manifest.

## Evidence and upstream references

Anonymous metadata access was checked on 2026-09-09 without acquiring model
weights: HF `Qwen/Qwen2.5-0.5B-Instruct` returned commit
`7ae557604adf67be50417f59c2c2f167def9a775`; ModelScope search and the corresponding
repository file listing both returned HTTP 200 and exposed per-file commit/hash
fields. These observations are not bandwidth or model-performance benchmarks.

- [Hugging Face Hub API](https://huggingface.co/docs/hub/en/api)
- [ModelScope official API examples](https://github.com/modelscope/modelscope-skills/blob/main/skills/ms-hub/SKILL.md)
- [ModelScope official SDK integration](https://github.com/modelscope/modelscope/blob/master/modelscope/hub/api.py)

Local tests exercise real small HTTP fixtures, including byte-range resume,
changed validators, malformed ranges, `416`, checksum failures, pause/cancel,
restart recovery, concurrency, disk admission, symlink refusal and auth/API
switches. The private fixture transport is test-only. Passing them does not imply
large multi-hour transfers or every hosting provider have been tested.

The standalone Firefox fixture checks search → file selection → plan → explicit
start/pause/resume/cancel, measured-byte display, source warnings and stopping
polling when hidden or disconnected. It uses fictional job receipts, not a live
model transfer. Native Go anonymous metadata calls were also run against both
public sources, separately from the mocked browser exercise.

```sh
go test -race ./internal/app -run '^TestDownload' -count=1
node --test web/tests/downloads.test.mjs
node web/tests/browser-downloads.mjs
# Optional, three public metadata calls; no model-file transfer:
HALOCLU_TEST_PUBLIC_METADATA=1 go test -v ./internal/app -run '^TestDownloadPublicMetadataOptIn$' -count=1
```
