# Protocol boundaries

| Capability | Chat | Responses | Anthropic | Gemini v1beta |
|---|---|---|---|---|
| Text, non-stream and SSE | Implemented | Implemented | Implemented | Implemented |
| Official SDK text/final helpers | Tested | Tested | Tested | Tested |
| Model list/details | Implemented | Chat catalog | Independent catalog | Independent catalog |
| Fixed JSON/limited Schema | Implemented | Implemented | Implemented | Implemented |
| One function call and one result | Implemented | Implemented | Implemented | Implemented |
| Full supported context to Chat upstream | Implemented | Translated | Translated | Translated |
| Image metadata/input | Parsed; AI capability required | Same | Same | Same |
| Simulated native reasoning | `reasoning_content` extension | summary item/events | Portable fallback | thought part |
| Signed/encrypted reasoning | Unsupported | Unsupported | Unsupported | Unsupported |
| Stateful/background Responses | — | Unsupported | — | — |
| Audio/video, multiple choices, tool execution | Unsupported | Unsupported | Unsupported | Unsupported |

Every ordinary preset must satisfy forced JSON or tool constraints, otherwise
it returns 400. Finite fixtures handle those cases; there is no arbitrary schema
generator. Schema validation disables external/file references and limits
depth/keywords. `propertyOrdering` and the observed Google Python alias are
checked annotations rather than guarantees of serialized key order.

Chat ends with a finish chunk, optional usage-only chunk and `[DONE]`.
Responses emits created/in-progress, output-item/part deltas and a complete final
object; max-token truncation uses incomplete. Anthropic opens/closes content
blocks, then emits message_delta/message_stop. Gemini uses data-only JSON
increments plus a finish/usage event and EOF. It never emits SSE comments or
other protocols' sentinels.

Pre-commit upstream errors retain HTTP error status or use the configured
backup. Mid-stream failures emit an error and close without successful terminal
markers. Some clients can interpret plain EOF as an incomplete-looking answer;
actual UI presentation remains a client-specific test.

Preset usage is deterministic UTF-8-size estimation, not provider tokenization.
Visible/reasoning counts do not overlap. Cross-Chat Anthropic streaming uses
estimated usage because its initial usage must precede the upstream final
statistics. `X-FakeAPI-Usage: estimated` marks those cases.

Native Chat reasoning is an extension, not an official universal field.
Untranslatable upstream reasoning is omitted across protocols by default, with
`X-FakeAPI-Reasoning: omitted-in-translation`. Portable upstream reasoning can
be explicitly configured except where JSON or strict tool constraints prevent it.

Sources and later contract corrections are in `docs/decisions/001-scope.md`.
