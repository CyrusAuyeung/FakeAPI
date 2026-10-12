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

## 已处理的互操作细节

- 空的 Chat `reasoning_content` 历史注解可重传；有内容的原生 reasoning 仍需独立支持。
- Gemini `safetySettings` 按[官方字段定义](https://ai.google.dev/api/generate-content#SafetySetting)
  校验后可用于本地预设。本地预设不运行供应商过滤器；带非空原生设置的 Chat 上游转发会明确拒绝。
- 客户端对新建本地 Provider 的回退标签可能记录缺少翻译的告警。
  验收单独保存这类已识别告警，其他运行时错误仍使验收失败。
- CI 安装中文字体，截图包含可读的中文消息。
