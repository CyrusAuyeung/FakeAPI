# Cherry Studio 配置与验收

验收固定到 [Cherry Studio 2.1.6](https://github.com/CherryHQ/cherry-studio/releases/tag/v2.1.6)，
使用官方 Linux x64 AppImage，并按发布资产的 SHA256 校验。
客户端启动在 Ubuntu 24.04 的隔离目录和虚拟显示中，不修改本机用户配置。

2026-10-12，[提交 6f4a8b6 的客户端验收](https://github.com/CyrusAuyeung/FakeAPI/actions/runs/38263916166)
通过全部列出的调用集和日志检查，rendererErrors/knownWarnings 均为空。

## 连接本地服务

先按 README 初始化 FakeAPI、创建 Key 并启动服务。进入 Cherry Studio 的模型供应商设置，
添加自定义供应商，填写刚创建的 Key。按希望使用的协议配置端点：

| Cherry Studio 端点类型 | 端点基址 |
|---|---|
| OpenAI Chat Completions | `http://127.0.0.1:8787/v1` |
| OpenAI Responses | `http://127.0.0.1:8787/v1` |
| Anthropic Messages | `http://127.0.0.1:8787/anthropic/v1` |
| Google Generate Content | `http://127.0.0.1:8787/v1beta` |

这些是 Cherry Studio 2.1.6 端点配置的 URL；其他 SDK 的 base_url 拼接方式见 README。
手动添加 `fakeapi-test` 和 `fakeapi-frog`，分别用于极速测试和奶蛙预设。
选择对应的端点类型，并设置上下文/输入上限 `32768`、最大输出 `8192`。
Anthropic 必须有有效的最大输出设置；模型未知且上限未设置时，客户端可能构造无效的 max_tokens。

先选 `fakeapi-test`，发送“只回答 OK”，再发送“1+1”；随后选 `fakeapi-frog`，发送 `/laugh`。
在聊天页面检查正常结束、字符画等宽显示，以及流式生成中的停止按钮。
该版本生成过程中以 **Pause** 按钮中断请求。

四张真实效果图、输入示例和复现命令见[效果展示](showcase.md)。

## 独立思考区域

使用 `preset` 模板的 `fakeapi-frog` 时，可以把配置中
`profiles.natural.thinking.channel` 从 `portable_text` 改为 `native`，保存后重启服务。
OpenAI Chat 此时通过 `reasoning_content` 输出带 `[模拟思考]` 标识的预设文案，正文只包含回复。
这是模拟内容；接入上游 AI 的行为另见[配置说明](configuration.md)。

Cherry Studio 2.1.6 会把独立推理内容放入“处理过程”区域，完成后通过
**Processed** 展开或收起，其中显示 **Deep reasoning**。
[原生通道录制验收](https://github.com/CyrusAuyeung/FakeAPI/actions/runs/38286857661)
已检查实际字段、独立显示、折叠/展开和流式字符画。
这一录制记录覆盖单次展示。`alpha.2` 起，API 接受非空 assistant 推理历史；
客户端验收另外增加了原生思考回复后的下一轮聊天。

## 自动验收范围

[Cherry Studio acceptance](https://github.com/CyrusAuyeung/FakeAPI/actions/workflows/cherry.yml)
使用真实未修改客户端，Provider 和模型通过客户端配置 API 写入隔离配置，聊天操作在实际界面执行：

- 四协议普通聊天的流式消费、消息完成状态、多轮历史和实际请求路径。
- 渲染后 ASCII 代码块与素材原文逐字符比较。
- 点击停止生成，再使用仅允许一个并发请求的 Key 发送新消息。
- 页面身份、非空界面、运行时异常、框架错误提示及截图。
- 请求日志仍只有四个字段，不包含正文和原始 Key。

测试报告和截图可从对应运行的 Artifacts 下载。通过范围以 report.json 的 passed 项为准。
Provider 添加界面的每个控件、模型自动发现、Windows/macOS 桌面版、其他版本和完整 Agent
流程不属于这一组自动验收。

## 重用工作流生成效果图

在仓库 Actions 中打开 **Cherry Studio acceptance**，选择 **Run workflow** 和 `main`，
再选择 `capture_mode`。无需为每次截图新建分支：

| 模式 | 内容 | 下载的 Artifact |
|---|---|---|
| `acceptance` | 原有四协议、正文、多轮和暂停恢复验收；也是 PR 默认模式 | `cherry-studio-2.1.6` |
| `screenshots` | 正常测试、默认字符画、挥手彩蛋和四个形象总览 | `cherry-showcase` |
| `streaming` | 原生思考通道、实际流式字符画和折叠/展开操作的 GIF | `cherry-native-streaming` |

命令行也可触发，例如 `gh workflow run cherry.yml --ref main -f capture_mode=screenshots`。
截图模式使用本地预设；GIF 模式在临时配置中启用 `native` 通道并调整输出节奏。
两种模式均不会调用真实 AI。录制文件只在 Artifact 中生成，不自动提交到仓库。

## 已处理的互操作细节

- Chat 可以返回独立 `reasoning_content`；`alpha.2` 起非空 assistant 注解也可重传，并按目标处理。
- Gemini `safetySettings` 按[官方字段定义](https://ai.google.dev/api/generate-content#SafetySetting)
  校验后可用于本地预设。本地预设不运行供应商过滤器；带非空原生设置的 Chat 上游转发会明确拒绝。
- 客户端对新建本地 Provider 的回退标签可能记录缺少翻译的告警。
  验收单独保存这类已识别告警，其他运行时错误仍使验收失败。
- CI 安装中文字体，截图包含可读的中文消息。
