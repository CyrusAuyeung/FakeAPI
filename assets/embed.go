// Package assets contains the distributable default material, never user imports.
package assets

import "embed"

//go:embed manifest.yaml nailong thinking text fixtures
var Files embed.FS
