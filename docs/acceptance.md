# 自动运行验收

仓库：[CyrusAuyeung/FakeAPI](https://github.com/CyrusAuyeung/FakeAPI)。
在 [Actions](https://github.com/CyrusAuyeung/FakeAPI/actions) 查看真实运行结果。
工作流在 main 分支提交、Pull Request 和手动运行时触发；发布工作流另外接受 `v*` 标签。

## 检查范围

| 工作流 | 验收 |
|---|---|
| Go and runtime acceptance | Linux、Windows、macOS 的 Go 测试、vet、原生 CLI/HTTP；Linux/macOS race；Linux 解码 fuzz 和 Docker Compose 运行 |
| Official SDK contracts | 六个锁定的 Python/Node 官方 SDK，普通/流式、历史、JSON、单工具、取消、模型列表和错误 |
| Build release | 五个平台归档、SHA256、架构与随包文档；在 Linux、Windows、macOS 解压对应发行包并实际运行 |

`scripts/runtime-smoke.py` 使用隔离的临时部署和 Key，验证：

- 初始化、配置校验，以及恰好四份 ASCII 形象。
- 四协议普通 JSON、SSE 解析、成功终止事件及次数扣减。
- 最新用户消息匹配、大笑彩蛋、模型发现不扣生成额度。
- 到期、总次数、RPM、在线修改及撤销 Key。
- 一致数据库备份、重启后额度和撤销状态持久化。
- 请求日志恰好四字段，不含测试正文或原始 Key。

容器验收使用仓库的 `compose.yaml`，另外检查非 root 用户、只读根目录、
只读配置、可写数据卷、删除全部 Linux capabilities 和 no-new-privileges。
每次使用独立 Compose project 和临时端口，结束后只删除本次创建的容器和数据卷。
测试用 override 需要 Docker Compose 2.24.4 或更新版本。

## 查看和重新运行

进入 Actions，选择工作流，点击 **Run workflow**，选择 main。
失败时查看红色 job 的具体 step；各运行的 Artifacts 提供 runtime JSON 报告，
Build release 还提供五份发行包及 SHA256SUMS。报告不包含 Key 或请求正文。

本地已构建可执行文件后：

```text
python scripts/runtime-smoke.py
python scripts/runtime-smoke.py --binary PATH_TO_EXTRACTED_BINARY
docker build -t fakeapi:ci .
python scripts/runtime-smoke.py --docker --image fakeapi:ci
```

常规验收不需要真实 AI、仓库 Secrets 或付费 Key。
main、PR 和手动构建生成可下载的测试发行包；只有明确推送 `v*` 标签才会发布
GitHub Release 和 GHCR 镜像。含预发行后缀的版本标记为 prerelease。

## 客户端验收边界

SDK 通过不能直接证明桌面客户端界面与 Provider 配置兼容。
Cherry Studio 是下一轮优先客户端，需要独立记录版本、四种 Provider、
模型发现、字符画显示、流式回复、停止生成及多轮历史结果。
Windows race、Linux/macOS arm64 原生运行和其他桌面客户端仍需要单独验收。
