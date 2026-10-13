# DeepSeek 混合模式

混合模式将带 `ai` 标签的 Key 的问候、身份询问、常见测试题和鹈鹕 SVG
请求交给真实上游，其他请求继续返回四个内置奶蛙形象。
匹配只看最新用户消息；命中上游后传入完整的受支持历史。

以下配置使用 `https://api.deepseek.com` 和 `deepseek-flash`，SVG 也使用同一模型。
模型名称会变化，部署时以 [官方模型文档](https://api-docs.deepseek.com/quick_start/pricing/)
和账户的 `/models` 返回为准。

2026-10-13 已使用真实 `deepseek-flash` 完成整轮验收：20 项检查通过，
其中 12 次成功 AI 调用，加一次无效凭据错误检查。
[脱敏结果报告](deepseek-results.json) 记录了具体检查、时间和所测二进制的 SHA256。
延迟是本次测试的观察值，模型内容与速度可能变化。

`alpha.2` 的新增功能已另行完成五次真实 AI 调用、七项检查：非空推理历史下一轮，
Chat/Responses/Anthropic/Gemini 的关闭思考，以及额度耗尽后的本地字符画。
[新增功能报告](deepseek-reasoning-results.json) 保留了对应二进制 SHA256；
使用 `python scripts/test-deepseek-reasoning.py` 可单独复测，无需重新生成 SVG。

## 初始化

`v0.5.0-alpha.2` 推荐使用 `fakeapi init --template deepseek --dir runtime/deepseek`。
这个模板已经设置端点、模型、能力和推理历史回传，只需设置上游 Key，详见
[平台模板](providers.md)。以下手动配置保留给使用旧 `hybrid` 模板的部署。

先从当前 main 源码构建；`reasoning_effort` 转发修复在 `v0.5.0-alpha.1` 发行包之后。

```powershell
go build -trimpath -o .\bin\fakeapi.exe .\cmd\fakeapi
.\bin\fakeapi.exe init --template hybrid --dir .\runtime\deepseek
```

编辑生成的 `runtime/deepseek/fakeapi.yaml`。在 `targets` 中，对
`primary-fast` 和 `primary-quality` 都设置以下字段，保留其原有的
`upstream: primary` 和 `model: {env: ...}`：

```yaml
max_tokens_parameter: max_tokens
developer_role_fallback: system
capabilities:
  images: false
  tools: true
  json_object: true
  json_schema: false
  developer_role: false
  parallel_tool_calls_parameter: false
```

此配置将入站 `developer` 消息转成 `system`，不向 DeepSeek 声称支持
`json_schema`。图片、强制工具及所有模型能力没有纳入这次真实验收。
能力开关和传参格式以 [官方 Chat API](https://api-docs.deepseek.com/api/create-chat-completion/)
及目标模型的实际支持为准。

在 PowerShell 中读取上游 Key，避免将它写进 YAML 或命令历史：

```powershell
$secret = Read-Host 'DeepSeek API Key' -AsSecureString
$env:FAKEAPI_UPSTREAM_API_KEY = [System.Net.NetworkCredential]::new('', $secret).Password
$env:FAKEAPI_UPSTREAM_BASE_URL = 'https://api.deepseek.com'
$env:FAKEAPI_UPSTREAM_MODEL = 'deepseek-flash'
$env:FAKEAPI_UPSTREAM_SVG_MODEL = 'deepseek-flash'
.\bin\fakeapi.exe config check --config .\runtime\deepseek\fakeapi.yaml
.\bin\fakeapi.exe keys create --config .\runtime\deepseek\fakeapi.yaml --policy hybrid --label deepseek-local --ai-limit 100
try {
    .\bin\fakeapi.exe serve --config .\runtime\deepseek\fakeapi.yaml
} finally {
    Remove-Item Env:FAKEAPI_UPSTREAM_API_KEY
    $secret = $null
}
```

客户端使用 `keys create` 显示的 **FakeAPI Key**，基址为
`http://127.0.0.1:8787/v1`。展示模型仍是 `fakeapi-test` 或 `fakeapi-frog`，
真实模型 ID 由 target 映射。用 `--policy demo` 创建的 Key 继续使用本地预设。

Linux/macOS 的 CLI 子命令相同，先用 `export` 设置上述四个环境变量。
程序不会自动加载 `.env`。

## 调用和思考

以下 Chat 请求命中真实问候规则，并关闭真实上游思考：

```json
{
  "model": "fakeapi-test",
  "messages": [{"role": "user", "content": "你好"}],
  "max_tokens": 512,
  "reasoning_effort": "none"
}
```

`reasoning_effort` 支持 `none/minimal/low/medium/high/xhigh/max`，会原样
传给 Chat 上游；具体值是否支持由上游决定。省略时保留上游默认设置。
DeepSeek 默认启用思考，当前官方接口支持用 `none` 关闭，见
[reasoning_effort 参数](https://api-docs.deepseek.com/api/create-chat-completion/)。

启用真实思考时，Chat JSON 的 `message.reasoning_content` 和 SSE 的
`delta.reasoning_content` 承载上游推理，正文位于 `content`。这部分来自
真实 DeepSeek，不使用预设的模拟思考文案。不要将推理拼进正文。

Responses、Anthropic、Gemini 入站也可走这个 Chat 上游适配器；混合模板
的 `reasoning_translation: omit` 会省略跨协议上游推理，保留正文。
这不等于提供这些供应商的原生签名思考。`alpha.2` 已将 Responses effort、
Anthropic disabled/adaptive 和 Gemini zero budget/level 映射到 DeepSeek 的控制；
精确的正 token 预算在该目标上明确拒绝，隐藏显示保持生成设置。

鹈鹕回复可能包含说明和 Markdown 代码块。验收会抽取其中的 `<svg>…</svg>`
检查 XML 完整性，API 保留原始正文，不强制模型只返回裸 SVG。
本地 ASCII 请求建议留足输出额度，例如 `max_tokens: 4096`，以显示完整形象。

## 可重复的真实验收

标准库 Python 脚本会创建独立临时部署、临时 FakeAPI Keys 和数据库，结束后
停止服务并清理部署。无需安装桌面客户端。使用已构建的二进制运行：

```powershell
python scripts/test-deepseek.py --binary .\bin\fakeapi.exe
```

脚本优先读取环境变量 `FAKEAPI_UPSTREAM_API_KEY`；若未设置，则在交互终端
隐藏输入。自动调用方可使用 `--key-stdin` 传入一行 Key，不提供 Key 命令行参数。

每轮最多 16 次有效模型生成：一般请求上限 768 tokens，SVG 上限 4096；
另有模型目录查询及一次无效凭据请求，用来检查真实上游认证错误。
这是显式的付费上游验收，没有自动重试，不会在普通 GitHub Actions 中运行。
失败后修正原因，再手动运行；重跑可能再次产生费用。

验证内容包括：

- 四种入站协议的 JSON、可解析 SSE 和各自的正常结束事件。
- 真实问候使用历史上下文，身份、数学和鹈鹕请求命中对应 AI 规则。
- Chat 思考开关、原生 `reasoning_content`、正文与 usage。
- 非空推理历史的下一轮真实聊天，以及其余三种入站协议的关闭思考映射。
- JSON object 模式；不支持的 JSON Schema 在调用前拒绝。
- 普通任务返回四个内置 ASCII 之一，`demo` Key 保持本地回答。
- AI 额度为零、耗尽后的拒绝及本地 ASCII 仍可用；成功/失败的次数扣减。
- 真实上游认证错误被转换成不含上游私有消息的 API 错误。
- 请求日志恰好 `time/model/duration_ms/rule` 四字段，不含 Key。

默认报告为 `.cache/deepseek-live/report.json`，可以通过 `--report` 改位置。
报告只保存状态、时间、模型、检查项和计数，不保存请求、回复或推理正文。
普通离线/CI 验收见 [运行验收](acceptance.md)。
