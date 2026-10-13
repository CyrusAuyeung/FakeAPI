package examples

import "embed"

//go:embed preset/fakeapi.yaml hybrid/fakeapi.yaml testing/fakeapi.yaml providers/*.yaml
var Files embed.FS
