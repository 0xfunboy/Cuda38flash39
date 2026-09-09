// Package webui contains only the compiled browser assets served by the gateway.
package webui

import "embed"

// Assets deliberately excludes tests, documentation and development tooling.
//
//go:embed index.html styles.css app.js ui-core.mjs assets/haloclu-icon.png assets/haloclu-horizontal.png
var Assets embed.FS
