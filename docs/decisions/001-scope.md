# Scope and implementation decisions

The supplied `FakeAPI_Development_Plan.md` is preserved as the original design.
The following subsequent user instructions take precedence:

- Bundle exactly four ASCII poses derived from the supplied references: waving,
  standing, laughing while holding the belly, and thinking. Do not bundle the
  initially proposed six generic frog drawings.
- Character copy uses laughter, holding the belly, thinking and breaking into
  laughter. Milk and croaking associations are excluded from product copy.
- Reference portraits use 64 columns. Natural pacing is 180 graphemes/second
  with 8-character chunks; playful pacing is 120 with 8-character chunks,
  0.02 pause probability and 300ms pauses. This fits the default 30s/45s budgets.

Go 1.27.2 is now available in the isolated project module cache and is the pinned
build/CI/container version. The initial scaffold was tested on Go 1.26.4 while
the default network mirror was unreachable; the final checks use 1.27.2.

The project uses standard-library HTTP/CLI, YAML v3, modernc SQLite, uniseg,
flock and a bounded JSON Schema validator. No runtime provider SDK is required.
SQLite stores Key metadata/hashes, counters and rate windows. Conversations,
images, responses and reasoning are never persisted.

Observed SDK contracts supersede illustrative design fields:

- Anthropic ModelInfo has `lifecycle: active`, `max_tokens`, `deprecated_at` and
  `retires_at`. Nullable capability/line fields remain null.
- Google Python 2.29.0 emits `property_ordering`; it is normalized to the known
  `propertyOrdering` annotation and validated as a property-name list.
- OpenAI Node 7.32.0 final helpers attach `parsed_arguments` and `parsed` to
  output items. Replay accepts null annotations, or non-null values verified
  against the original JSON; annotations never replace the original content.

References checked 2026-10-12:

- https://developers.openai.com/api/reference/resources/chat/subresources/completions/streaming-events
- https://developers.openai.com/api/reference/resources/responses/streaming-events
- https://platform.claude.com/docs/en/build-with-claude/streaming
- https://ai.google.dev/api/generate-content
- Installed, locked official SDK type and stream implementations.
