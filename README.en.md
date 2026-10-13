# FakeAPI

GitHub Actions verifies native runtime and race on Linux, Windows, and macOS,
Linux Docker Compose, six official SDKs, and extracted release archives.
Cherry Studio 2.1.6 Linux x64 also passed four-protocol streams, history,
rendered ASCII consistency, and cancellation recovery in an isolated profile.
See [acceptance](docs/acceptance.md), [compatibility evidence](docs/compatibility.md),
and [client configuration](docs/cherry-studio.md) for the tested scope.
The [Windows PowerShell tutorial](docs/windows-quickstart.md) needs no AI desktop client.

A small, self-hosted AI API simulator with configurable presets, streaming,
timing, JSON/function fixtures, and selective calls to real AI.

Development preview: `0.5.0-alpha.2`. Six pinned official Python/Node SDKs have
passed the documented local contract checks. GUI and container validation is
tracked separately in [compatibility](docs/compatibility.md).

## Run

Requires Go 1.27.2 for source builds. Releases can run without Go.
Download the [development preview](https://github.com/CyrusAuyeung/FakeAPI/releases/tag/v0.5.0-alpha.2)
and follow the instructions included in the extracted archive.

```sh
go build -o bin/fakeapi ./cmd/fakeapi
bin/fakeapi init --template preset --dir runtime/preset
bin/fakeapi config check --config runtime/preset/fakeapi.yaml
bin/fakeapi keys create --config runtime/preset/fakeapi.yaml --policy demo --label local --expires-in 30d
bin/fakeapi serve --config runtime/preset/fakeapi.yaml
```

Save the generated Key. It is shown only once; the database stores its hash.
On Windows, build and run `bin/fakeapi.exe`.
For a downloaded archive, skip the build and use `./fakeapi` (or `./fakeapi.exe`)
from its extracted directory instead of `bin/fakeapi`. No Go/Python/Node runtime
is needed for the packaged binary.

| Protocol | Client base URL |
|---|---|
| OpenAI Chat / Responses | `http://127.0.0.1:8787/v1` |
| Anthropic Messages | `http://127.0.0.1:8787/anthropic` |
| Gemini Generate Content v1beta | `http://127.0.0.1:8787` |

Models: `fakeapi-frog` (natural) and `fakeapi-test` (instant).
Anthropic requires `anthropic-version: 2023-06-01`. Gemini streaming uses
`:streamGenerateContent?alt=sse`. Model IDs and catalogs are configurable.

## Behavior

Only the newest semantic user message participates in rule matching. Greetings,
identity, bounded arithmetic and explicitly listed benchmark prompts have local
answers; other prompts return one of four selected character poses. Pictures
are not downloaded or interpreted by presets. Laughter and thinking define the
character copy. The original reference images are not bundled.

Initialize with `--template hybrid` for selective real-AI calls. Set
`FAKEAPI_UPSTREAM_BASE_URL`, `FAKEAPI_UPSTREAM_API_KEY`,
`FAKEAPI_UPSTREAM_MODEL`, and `FAKEAPI_UPSTREAM_SVG_MODEL`, then create a Key
using `--policy hybrid`. Four AI rules apply only to Keys tagged `ai`. Matched
AI calls forward complete supported context to an OpenAI Chat compatible
upstream or the native Claude Messages adapter. Targets can map display models to multiple providers/models.

`init --template deepseek|gpt|claude|gemini|glm|kimi` supplies six provider overlays
with endpoints, models, reasoning controls and capability limits. Set only
`FAKEAPI_UPSTREAM_API_KEY`. See [provider setup](docs/providers.md).

For an offline upstream: `go run ./tests/mockupstream/cmd`; use
`http://127.0.0.1:9099/v1`, an artificial upstream key, `mock-fast`, and
`mock-svg`. The binary does not load `.env` automatically.

## Manage and test

Run `fakeapi help`. Key commands include create/import/list/show/update/revoke,
expiry, quota/rate limits and explicit usage reset. Null/unlimited differs from
zero. All relative paths resolve against the configuration directory. Restart
after changing configuration or assets.

```sh
go test ./...
go vet ./...
python -m pip install -r tests/sdk/python/requirements.txt
npm ci --prefix tests/sdk/node
python scripts/test-sdk.py
```

The SDK runner expects a built `bin/fakeapi` (or `bin/fakeapi.exe` on Windows).
It creates and removes a temporary local deployment. No paid AI is needed.
CI also exercises an immutable previous binary, same-schema downgrade, backup
recovery and future-schema rejection. [Performance observations](docs/performance.md)
measure real localhost requests with SQLite accounting and logs enabled; no speed gate is imposed.

Docker: build with `docker compose build`, export a preset to `runtime/` with
the image's `init --shared-config` command, create a Key with `docker compose run`, then start
with `docker compose up -d`. SQLite is a persistent named volume; configuration
is read-only and the service runs as UID 65532.
Shared initialization makes only generated configuration and materials readable
by that separate user. It creates no credentials or database; ordinary local
initialization keeps private file permissions.

Logs contain exactly time, validated model, duration and matched rule. SQLite
stores Key metadata/hashes and counters, never conversations. Admitted failures
and cancellations remain counted. Model discovery and health checks are free.

Text core, SSE, finite JSON and one-function round trips are implemented.
Chat reasoning history, unsigned summary/thought replay and native Claude signed
blocks on an Anthropic-to-Claude path are supported. Stored Responses,
cross-provider signatures, audio/video, multiple candidates and full agent
execution are outside this version. See [protocol matrix](docs/protocol-matrix.md).

MIT covers software. Character-reference material and imported assets have
separate provenance; see [asset notices](docs/ASSET_NOTICES.md) and
[third-party notices](docs/THIRD_PARTY_NOTICES.md).
