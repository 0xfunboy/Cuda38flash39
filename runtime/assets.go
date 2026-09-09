// Package runtimeassets provides the audited catalog compiled into the gateway.
// Runtime launchers and model weights are not embedded in the executable.
package runtimeassets

import _ "embed"

// ModelCatalogJSON is the curated inventory; requests cannot supply commands,
// download URLs or destination paths in place of this embedded catalog.
//
//go:embed model-catalog.json
var ModelCatalogJSON []byte
