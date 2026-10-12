# 实施状态

依据工作目录中的 `FakeAPI_Development_Plan.md` 实施；后续用户补充覆盖原计划，
具体决定见 [001-scope](decisions/001-scope.md)。当前为 `0.5.0-alpha.1` 开发预览版。

已实现：四种入站协议、普通与 SSE 输出、模型目录、最新语义用户消息匹配、
规则优先级与覆盖、三种节奏、可选模拟思考、四份指定 ASCII、素材导入、
有限 JSON 与单函数 fixture、选择性 AI/完整受支持历史转换、超时与预提交备用回复。

Key 管理包含创建/导入/查询/更新/撤销、到期、总与 AI 次数、RPM、并发限制和显式重置。
SQLite 事务保护准入，失败和取消保留已提交次数。请求日志只有
time/model/duration_ms/rule。CLI 包含配置检查、规则解释、素材校验、一致备份和健康检查。

## 本地验证

- Go 单元和 HTTP 集成测试、`go vet`、真实 SDK 的指定调用集。
- 总额度/AI 额度并发争抢、重启、时钟回退、过期、撤销、SQLite busy 与事务回滚。
- 四协议的响应/错误/SSE 生命周期、Schema、Unicode、取消清理和上游失败策略。
- Go `testing/synctest` 验证逻辑节奏，真实 localhost 请求验证传输取消。
- 严格 YAML、生成配置 Schema 与示例的一致性、四份素材与静态等宽预览。
- Windows 可执行文件与五个目标平台的交叉构建；归档附文档、素材、许可和 SHA256。
- 固定旧二进制与候选版本的升级、同 schema 回退、在线备份恢复、RPM 保留及未来 schema 拒绝。
- 真实 Windows PowerShell JSON/中文 UTF-8/字符画/SVG 调用；明确 UTF-8 响应编码。
- 请求限制与不扣额度、服务停止/取消释放，以及四协议流中失败和目录游标检查。
- 开启持久计数和日志的预设/模拟上游性能观察，见 [performance](performance.md)。

覆盖率由 `go test -coverpkg=./internal/... -coverprofile=coverage.out ./...`
产生，可用 `python scripts/coverage-summary.py` 汇总。统计包含 Go 进程内集成测试，
不包含外部 SDK 进程的二进制覆盖率。编码、规则、额度的关键行为已有断言；
整体覆盖率仍低于全量完善后的目标，其他拒绝分支与故障注入需要继续补充。

Linux amd64 Docker Compose、三系统 race、Linux amd64/Windows amd64/macOS arm64
原生运行及对应发行包解压运行已经通过 GitHub Actions；六套 SDK 也已在远端通过。
代码已公开到 [CyrusAuyeung/FakeAPI](https://github.com/CyrusAuyeung/FakeAPI)。
客户端与其他架构的状态及证据见 [兼容记录](compatibility.md)，
自动验收操作见 [acceptance](acceptance.md)。当前采用开发预览标签，v1.0 门槛仍需补齐。
发行包是开发版构建，不等同于完成原计划全部 P8 验收。
