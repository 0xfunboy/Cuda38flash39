# Pinned external inference engine

The product controller and gateway are Go. CIRU/vLLM/torch/ROCm remain the
already-installed external engine; no weights/frameworks are copied here.

`manifest.json` pins model/drafter revisions, rank shard layout, existing SHA-256
receipts and the actual qualified patched runtime files. Startup rehashes only
these small runtime/native files and checks existing shard lengths. It does not
rehash 180 GB of unchanged weights or download anything. These recorded weight
hashes are provenance from prior qualification, not a claim of a fresh rehash.

Runtime: TP2/PP1; Socket on thunderbolt0; DFlash2 k5/local0; 64K engine profile;
prefix cache OFF; safe-prefill/canonical-MoE/stable routing/coherent tiles/F1-WMMA.
NHI/RDMA and approximate paths remain OFF. `launch-rank.sh` provides the original
qualified settings without importing the old Python controller or frontend.

Upstream sources are CIRU's published GLM Flash runtime/model and incoai's
DFlash2 checkpoint at the immutable revisions in the manifest. Their licenses
and notices remain with the external installation; no ownership of those
frameworks, kernels or model weights is implied by this product.

## Rebuild recipe (manual, separate environment only)

1. Use the pinned CIRU source archives/wheels and requirements named by the
   manifest. Follow their included upstream INSTALL-RUNTIME instructions in a
   separate engine root; do not install over the live environment.
2. In its installed `vllm` package apply these patch files, in order, with
   `patch --dry-run -p1` first: safe-prefill, canonical-moe, stable-ties,
   uniform-tiles, f1-wmma.
3. Compare the resulting target files to `manifest.json` identities. A mismatch
   is a new unqualified runtime, not permission to relax the manifest.
4. Point a separate controller configuration at that installation; qualification
   and a whole-pair restart are required before changing the live preset.

No build or privileged installation is performed automatically. Patch files are
the five small local correctness fixes; rejected campaigns are not included.

## Whole-pair ownership and rollback

The Go controller uses distinct `strixglm-rank{0,1}` unit names and rank port18110.
It refuses to start while any old rank or frontend is active. A durable owner
reservation precedes every systemd write; nonce and InvocationID must agree
before any stop. Native paired admission holds the product pair lock; the
gateway holds the distinct legacy pair lock. Controller management acquires
both, non-blocking. The gateway must never hold the product pair lock while
calling the native coordinator. Poison is archived only after all owned ranks
are proven stopped.

The explicit legacy snapshot/stop/restore operations preserve the original
systemd argv, properties, owner metadata, and retention definitions without
copying the engine or weights. Restore launches the entire old pair before its
old frontend, updates InvocationIDs, and retains original ports18091/18092.
The old Python frontend is used only for this explicitly selected rollback,
never by the new native paired backend. Do not expose lifecycle commands as
unauthenticated HTTP endpoints. No automatic takeover/switch is performed.

Native gateway/coordinator services are separate from the two owned rank units.
Stopping or restarting those HTTP services does not stop/restart a rank, and
cluster stop/restart does not manage the HTTP services. Close admission and
drain those services before a maintenance operation; see OPERATIONS.md.
