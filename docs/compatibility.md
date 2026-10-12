# 兼容验证记录

FakeAPI `0.5.0-dev`，验证日期 **2026-10-12**。本地系统为 Windows 11
build 26200，Go 1.27.2、Python 3.13.11、Node 24.12.0。
所有服务端和上游测试使用 localhost，未调用付费 AI。

## 官方 SDK

| SDK | 锁定版本 | 验证范围 | 结果 |
|---|---|---|---|
| OpenAI Python | 3.29.0 | Chat/Responses create 与 stream、最终对象 helper、历史重传、JSON/Pydantic、单工具往返、models、错误、显式 close | 通过 |
| OpenAI Node | 7.32.0 | Chat/Responses create 与 stream、finalResponse、历史重传、JSON、单工具往返、models、错误、Abort | 通过 |
| Anthropic Python | 1.13.0 | Messages create/stream/final message、JSON、单工具往返、models、错误、显式 close | 通过 |
| Anthropic Node | 0.133.0 | Messages create/stream/finalMessage、JSON、单工具往返、models、错误、Abort | 通过 |
| google-genai Python | 2.29.0 | generate/stream、chat 两轮、双字段 Pydantic Schema、单工具往返、models、错误、同步/异步关闭 | 通过 |
| @google/genai Node | 2.28.0 | generate/stream、JSON、单工具往返、models、错误、AbortController | 通过 |

测试入口为 `scripts/test-sdk.py`，调用两个 `tests/sdk/*/smoke` 脚本。
脚本创建临时部署与临时 Key，捕获输出时遮盖 Key，并检查服务器日志。
完整 Python 和 Node 依赖分别锁定在 requirements.txt 和 package-lock.json。

“通过”限定为表中调用集，不表示实现了供应商的全部 API。
JSON 和工具 fixture 使用 `fakeapi-test` 模型及带 `test` 标签的 Key。
SDK 中的工具测试不执行外部工具，也不开启自动工具循环。

## 协议边界

- Chat 的 `reasoning_content` 是扩展字段。模拟思考默认使用普通正文；
  Responses/Gemini 可输出各自的 summary/thought 表示。
- Anthropic 不生成 thinking signature。Chat 上游转换为 Anthropic 的流式
  usage 统一估算，响应头包含 `X-FakeAPI-Usage: estimated`。
- Responses 使用客户端重传上下文；有状态/后台/加密 reasoning 暂缓。
- SDK helper 的 `parsed`/`parsed_arguments` 仅作为经过一致性验证的重传注解。
- Chat 历史中的空 `reasoning_content`（null/空字符串）可重传；非空值仍显式拒绝。
- Gemini 使用 data-only SSE，结束时发送 finishReason/usage，再关闭流。
  不发送 `[DONE]` 或注释心跳。中途失败不发送成功终片；消费方应同时检查
  结束原因，全部 SDK 中途错误表现仍需补齐专项记录。
- 入站支持四类协议，真实 AI 上游第一版采用 OpenAI Chat 兼容协议。
  不可无损翻译的内容会在扣次数之前拒绝。
- Gemini 的 `safetySettings` 校验类别、阈值和重复项后可用于本地预设。
  本地预设不执行供应商过滤器；非空设置没有 Chat 上游对应参数，AI 路由在准入前拒绝。

Gemini 新式 `responseFormat.text.mimeType` 使用 `APPLICATION_JSON` 或
`TEXT_PLAIN` 枚举，并检查新旧格式冲突；协议定义见
[Google 官方 API](https://ai.google.dev/api/generate-content#TextResponseFormat)。

## 待验证环境与客户端

| 项目 | 状态 / 原因 |
|---|---|
| Docker Linux amd64 构建与 Compose 运行 | GitHub Actions 通过，包含四协议 HTTP、额度、备份、重启、权限和日志验证 |
| Linux/Windows/macOS race detector | 三系统 GitHub Actions 通过 |
| Linux amd64、Windows amd64、macOS arm64 原生 CLI | 三系统 Go 测试、原生运行与发行包解压运行通过 |
| Linux arm64、macOS amd64 原生 CLI | 归档/架构已校验，仍待对应机器实际运行 |
| Cherry Studio 2.1.6 | 独立 PR 中验收四协议、多轮、字符画和取消；结果以客户端工作流为准 |
| Open WebUI Chat | 待 Docker 环境实测 |
| aider、Cline | 待固定版本实测 |
| Codex CLI、Claude Code | 待探索验证；不承诺完整 Agent 工作流 |

这些待验证项保留了开发计划的发布门槛。当前使用开发版本号，尚未声明 v1.0。

## GitHub Actions 证据

已完成的基线提交为 `95ef6d5`（2026-10-12），远端结果：

- [三系统 Go、race、fuzz 和 Docker Compose](https://github.com/CyrusAuyeung/FakeAPI/actions/runs/38256188222)。
- [六套官方 SDK](https://github.com/CyrusAuyeung/FakeAPI/actions/runs/38256188223)。
- [五份发行包与三系统解压运行](https://github.com/CyrusAuyeung/FakeAPI/actions/runs/38256188269)。

运行报告和测试发行包在对应 Actions 的 Artifacts 中。容器初始化使用
`init --shared-config`；普通本地初始化仍保留私有权限。
验收修复了 Windows 短路径/macOS 链接父目录下的素材导入误判，并保留导入越界拒绝。
详细步骤见 [自动运行验收](acceptance.md)。
