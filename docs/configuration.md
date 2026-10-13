# Configuration v1

`fakeapi config check` performs offline syntax, reference, asset, profile, Schema
and enabled-upstream environment checks. `--skip-env` only skips environment
presence. `config show --redact` preserves environment references and does not
resolve them into secret values. `config schema` emits the schema generated
from the same Go types; semantic reference/action checks remain in the loader.

Three complete examples are distributed: preset, hybrid and deterministic
testing. `init` rewrites their relative paths for the new deployment directory.
Six provider overlays (`deepseek/gpt/claude/gemini/glm/kimi`) inherit the hybrid
example during initialization; only an upstream credential is required.
The original development plan is preserved, with later changes recorded in
`docs/decisions/001-scope.md`.

## Matching and actions

Matchers have positive `any/all`, negative `none`, and optional `max_chars`.
Atomic conditions are `exact`, `keyword` and Go RE2 `regex`. Text is normalized
to LF and trimmed only at the edges. Images and nested tool results do not
become matching text. Empty text never matches ordinary text matchers.

Rules are ordered by descending priority, then configuration order. `when`
combines text matcher references with model IDs and trusted Key tags/groups.
There is one mandatory preset fallback. Key tags come from SQLite; headers
cannot grant AI access.

Actions: preset text/asset/weighted choices, bounded arithmetic, fixed JSON,
single-function fixture, or upstream. Only `{{model}}` and `{{rule_id}}` are
supported in inline text. Uploaded/ASCII/SVG/JSON assets are not templated.

Profile resolution: global → model → rule profile → partial timing/thinking
override. Native simulated reasoning is partial support; default is labeled
ordinary text. Strict text, SVG, JSON, arithmetic and tool fixtures disable
simulated reasoning. `thinking: {enabled: false}` is a valid partial override.

## AI routing

Target resolution: rule action target → model target → default target. Upstreams
hold connection settings; targets hold real model IDs and capabilities. Disabled
AI rules may omit a target. Enabled unresolved targets fail startup.

`kind: openai_chat` and native `kind: anthropic` are implemented upstream. Explicit capability flags
govern images, tools, JSON and developer-role conversion. Base URLs must be
HTTP(S), without userinfo/query/fragment. Secrets are environment references.
No automatic provider discovery, retries or provider failover occurs.

Reasoning controls from all four inbound protocols are normalized, validated
against `targets.*.reasoning`, and translated to provider parameters before
quota admission. Display controls do not disable generation. Exact budgets
require a supporting target. Omitted controls preserve provider defaults.
Chat assistant reasoning can be preserved/omitted/rejected per target; unsigned
summary/thought replay stays a display annotation. Native Claude signed blocks
are preserved only on an Anthropic-to-Claude path. See [provider setup](providers.md)
and [DeepSeek live acceptance](deepseek.md).

Failure policy is `api_error` by default, or a finite `preset`/`frog` backup.
Backup is allowed only before any downstream response is committed, and within
the original request's remaining time/constraints. A failed partial stream is
closed with an error, without a success marker or replacement character.

## Material and persistence

`assets/manifest.yaml` registers IDs, kinds, author, origin and license.
Only the four selected ASCII poses are bundled. ASCII imports are limited to
80 columns; default portraits are 64. Text must be UTF-8/LF with a final newline
and without tabs/control characters. Imported paths remain under `imports/`.

Configuration paths resolve relative to the YAML file; asset paths resolve
relative to the manifest. Configuration is immutable after startup. CLI Key
changes are live; rule/asset changes require restart.
