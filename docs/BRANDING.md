# Brand assets and web previews

HaloClu is the product name. StrixHaloClusterGLM identifies the GLM reference
deployment and repository.

## Asset library

| Asset | Use |
| --- | --- |
| `web/assets/haloclu-icon.png` | Transparent 800 × 800 application symbol |
| `web/assets/haloclu-horizontal.png` | Transparent 2508 × 627 sidebar wordmark |
| `web/favicon.ico` | Browser icon with 16, 32, 48, 64, 128 and 256px frames |
| `web/assets/haloclu-social.png` | 1729 × 910 social preview |
| `docs/assets/haloclu-header.png` | README header |

The symbol and wordmark are the transparent PNGs supplied by the product owner.
The repository uses those files unchanged. The favicon is derived from the
square symbol with its alpha channel preserved.

The social card extends the supplied header's charcoal, off-white and copper
visual style. It carries the HaloClu name and the text “Local AI. Paired Strix
Halo.” and “Chat · Pi coding · Models”. The original header is retained.

## Updating logos

Replace the two PNGs in `web/assets/`, then rebuild the favicon:

```sh
go run ./tools/favicon -input web/assets/haloclu-icon.png -output web/favicon.ico
```

Update the asset version query in `web/index.html` so browsers request the new
artwork, and rebuild the gateway with `make build`. Frontend files and artwork
are embedded in the Go binary; deploy the new gateway after its work has drained.
The inference ranks remain running during an interface-only update.

## Social preview configuration

The initial HTML includes the page description, Open Graph metadata and an X
large-image card. Image dimensions and alternative text are supplied with the
preview asset.

Set `HALOCLU_PUBLIC_URL` in the gateway's environment to the deployment's
public HTTP(S) base URL before starting it, for example:

```sh
HALOCLU_PUBLIC_URL=https://halo.example/ ./bin/strixglm serve --config config.json
```

The gateway validates this URL at startup and uses it for absolute image links,
`og:url` and the canonical link. Without the setting, assets use relative links.
The URL identifies an already-provisioned deployment; network exposure and TLS
are managed separately. Social crawlers require a publicly reachable page and
image, so a loopback installation cannot provide an external link preview.

For a GitHub repository preview, upload `haloclu-social.png` in the repository's
social preview settings.

[Open Graph specification](https://ogp.me/) · [Security configuration](../SECURITY.md)

## Product screenshots

The README images are browser captures of the deployed interface. They cover
Chat, Coding, Models, Benchmark, Cluster and Options, plus a download detail.
Conversation titles are hidden in the capture browser to protect user privacy;
capture sessions do not submit inference requests or start operations.

Refresh the capture set with:

```sh
node web/tests/browser-live-readonly.mjs http://127.0.0.1:18093/ state/api-token /path/to/captures
```

The harness checks live UI bindings, uses a temporary browser login and signs out
after capture. Review the images before copying them to `docs/assets/`.
