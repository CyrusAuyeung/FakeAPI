# Contributing

Use Go 1.27.2. Run `gofmt`, `go vet ./...` and `go test ./...`.
Protocol changes also run `scripts/test-sdk.py` with the locked SDK dependencies.
Tests use artificial input, temporary Keys and localhost upstreams.

Keep rules, protocol encoding, quota persistence and upstream translation in
their separate modules. Add boundary/error/cancellation coverage for behavioral
changes. Declare capability limits instead of discarding unsupported context.
Never put private conversations, images, real keys or runtime SQLite files into
fixtures. Production request logs remain exactly four fields.

Add assets through the manifest with source and license; keep the four default
character poses unless the maintainer changes scope. Source templates derive
from the original plan with later user changes in the extraction script.

New providers/protocol capabilities need a conversion contract, official-source
references, offline mock tests and a precise compatibility record.
