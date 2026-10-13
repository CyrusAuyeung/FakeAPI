# 平台模板与思考设置

从 `v0.5.0-alpha.2` 开始，以下六个模板继承混合模式的规则、Key 策略和四个内置形象。
端点、模型和能力已经填好；只需给服务进程设置 `FAKEAPI_UPSTREAM_API_KEY`。
模型和账户权限可能变化，替换模型时同时检查 `targets.*.capabilities` 和 `reasoning`。

| 模板 | 默认模型 | 上游协议 | 关闭思考 | 原生强度 |
|---|---|---|---|---|
| `deepseek` | `deepseek-flash` | OpenAI Chat | 支持 | none/minimal/low/medium/high/xhigh/max |
| `gpt` | `gpt-6-luna` | OpenAI Chat | 支持 | none/low/medium/high/xhigh/max |
| `claude` | `claude-sonnet-4-6` | 原生 Messages | 支持 | none 关闭；low/medium/high/max |
| `gemini` | `gemini-3.8-flash` | Google OpenAI 兼容接口 | 不支持 | minimal/low/medium/high |
| `glm` | `glm-5.2` | Z.AI OpenAI 兼容接口 | 支持 | none/minimal/low/medium/high/xhigh/max |
| `kimi` | `kimi-k3` | Moonshot OpenAI 兼容接口 | 不支持 | low/high/max |

模板文件在 `examples/providers/`，初始化时与 `hybrid` 合并。生成的 YAML 是完整配置，
修改它后重启即可。软件不会自动发现模型能力，也不会自动加载 `.env`。
GLM 模板使用 Z.AI 国际端点；国内智谱和 Coding Plan 的 Key/地址须按对应账户配置。
Kimi 使用 `api.moonshot.ai`；国内账户可以按官方说明切换区域端点。

## 启动

例如部署 DeepSeek；其他平台只需将模板名及目录名换掉：

```powershell
.\fakeapi.exe init --template deepseek --dir .\runtime\deepseek
$secret = Read-Host 'Upstream API Key' -AsSecureString
$env:FAKEAPI_UPSTREAM_API_KEY = [System.Net.NetworkCredential]::new('', $secret).Password
.\fakeapi.exe config check --config .\runtime\deepseek\fakeapi.yaml
.\fakeapi.exe keys create --config .\runtime\deepseek\fakeapi.yaml --policy hybrid --label local --ai-limit 100
try {
    .\fakeapi.exe serve --config .\runtime\deepseek\fakeapi.yaml
} finally {
    Remove-Item Env:FAKEAPI_UPSTREAM_API_KEY
    $secret = $null
}
```

从源码启动时先 `go build -o bin/fakeapi.exe ./cmd/fakeapi`，将命令中的程序路径改为
`bin/fakeapi.exe`。Linux/macOS 使用对应可执行文件和环境变量，子命令相同。

客户端填写创建出的 **FakeAPI Key** 和 [协议基址](../README.md#快速启动)，
展示模型仍是 `fakeapi-test` / `fakeapi-frog`。仅带 `ai` 标签的 Key 的指定规则走上游；
普通任务继续返回字符画。上游 Key 不发给客户端。

## 四种入站协议

| 设置意图 | Chat | Responses | Anthropic | Gemini |
|---|---|---|---|---|
| 关闭生成思考 | `reasoning_effort: "none"` | `reasoning: {effort: "none"}` | `thinking: {type: "disabled"}` | `thinkingConfig: {thinkingBudget: 0}` |
| 强度 | `reasoning_effort` | `reasoning.effort` | `output_config.effort` | `thinkingConfig.thinkingLevel` |
| 只隐藏显示 | — | `reasoning.summary: "none"` | `thinking.display: "omitted"` | `thinkingConfig.includeThoughts: false` |

Gemini 的 `thinkingConfig` 位于 `generationConfig` 内。隐藏显示会保留上游生成设置，
因此仍可能产生思考 token。关闭生成必须被目标模型支持；Gemini 3.8 和 Kimi K3 模板
会在调用、扣额度前拒绝关闭思考的请求。强度不在目标白名单内也会明确返回 400。

正的 token 预算会原样映射到支持它的 Claude/Google 目标，不会猜成某个强度。
Claude 预算至少 1024 且小于 `max_tokens`；Gemini 的预算 `-1` 表示动态思考。
默认 Gemini 3.8 模板使用 thinking level，不接收 token 预算；改用预算模型时需同步配置。

`targets.*.reasoning` 包含：

- `mode`: `effort`、`thinking`、`thinking_effort`、`google`、`anthropic` 或 `unsupported`。
- `allowed_efforts` / `can_disable`: 当前目标模型接受的强度及关闭能力。
- `default_effort`: 跨协议请求只要求开启思考、没有强度或预算时的选择。完全省略控制时保留上游默认。
- `budget`: 是否允许精确 token 预算。
- `history`: `preserve` 原样发送 Chat 推理历史，`omit` 省略显示注解，`reject` 拒绝推理历史。

## 多轮历史

Chat 的 assistant `reasoning_content` 可重传，规则仍只匹配最新用户消息。
DeepSeek、GLM 和 Kimi 模板保留它；GPT 和 Gemini 模板省略不属于其输入契约的普通注解。
Responses 的普通 summary 和 Gemini 无签名 thought 可重传为显示注解，不会冒充供应商的隐藏状态。

Claude 原生 Messages 入站到 Claude 上游可原样回传 `thinking` 的文本/签名及
`redacted_thinking.data`，保留原始块顺序。流式签名思考块收齐后输出，正文继续增量发送。
Chat 调 Claude 时可显示文本推理；Chat 的纯文本历史不带 Claude 签名，转入 Claude 时会省略该注解。
跨供应商签名、Gemini thought signature 和 Responses 加密状态仍明确拒绝。

模板默认关闭图片。GPT/Gemini 的 Chat 工具能力保守关闭；其他上游沿用单函数限制，
不执行工具，不支持多个并行函数或混合正文与函数输出。
Claude 模板保守关闭 JSON 约束；可用本地 JSON fixture。完整 Structured Outputs 尚未实测。

## 验收与来源

六模板已纳入本地契约测试：每个平台四协议 JSON/SSE、思考控制、次数扣减、
不支持的关闭请求不扣额度，以及 Chat/Claude 推理历史后继续本地回复。
Claude 另有签名、错误、超时、流结束和输出上限测试。
这些测试使用本地替身，不代表六个平台均完成账户真实联调。
真实 DeepSeek 检查和手动脚本见 [DeepSeek 联调](deepseek.md)。

接口核对日期：2026-10-13。各平台官方依据：

- [DeepSeek Chat API](https://api-docs.deepseek.com/api/create-chat-completion/)
- [OpenAI GPT-6 Luna](https://developers.openai.com/api/docs/models/gpt-6-luna)
- [Claude Messages](https://platform.claude.com/docs/en/api/messages/create) / [effort](https://platform.claude.com/docs/en/build-with-claude/effort)
- [Google OpenAI 兼容接口](https://ai.google.dev/gemini-api/docs/openai)
- [Z.AI Chat API](https://docs.z.ai/api-reference/llm/chat-completion)
- [Kimi reasoning effort](https://platform.kimi.ai/docs/guide/use-reasoning-effort) / [thinking](https://platform.kimi.ai/docs/guide/use-thinking-models)
