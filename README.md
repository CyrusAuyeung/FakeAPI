# FakeAPI

[![运行验收](https://github.com/CyrusAuyeung/FakeAPI/actions/workflows/ci.yml/badge.svg)](https://github.com/CyrusAuyeung/FakeAPI/actions/workflows/ci.yml)
[![官方 SDK](https://github.com/CyrusAuyeung/FakeAPI/actions/workflows/sdk.yml/badge.svg)](https://github.com/CyrusAuyeung/FakeAPI/actions/workflows/sdk.yml)
[![发行包验收](https://github.com/CyrusAuyeung/FakeAPI/actions/workflows/release.yml/badge.svg)](https://github.com/CyrusAuyeung/FakeAPI/actions/workflows/release.yml)

轻量、可自部署的 AI API 模拟服务。用规则返回预设文案、字符画、JSON 或单次工具调用，也可以只把指定测试请求交给真实 AI。

当前为 `0.5.0-alpha.1` 开发预览版。已实现四种入站协议的文本核心和流式输出，并通过六个锁定的官方 SDK 的指定调用测试。桌面客户端和容器的完整发布验收状态见 [兼容记录](docs/compatibility.md)。

Cherry Studio 2.1.6 Linux x64 已通过四协议流式、多轮、ASCII 正文和暂停恢复验收。
客户端端点与模型上限的填写方法见 [Cherry Studio 配置](docs/cherry-studio.md)。
没有 AI 客户端也可使用 [Windows PowerShell 教程](docs/windows-quickstart.md)。

## 快速启动

源码需要 Go 1.27.2。普通部署可以使用对应平台的可执行文件。

Windows PowerShell：

```powershell
go build -trimpath -o .\bin\fakeapi.exe .\cmd\fakeapi
.\bin\fakeapi.exe init --template preset --dir .\runtime\preset
.\bin\fakeapi.exe config check --config .\runtime\preset\fakeapi.yaml
.\bin\fakeapi.exe keys create --config .\runtime\preset\fakeapi.yaml --policy demo --label local --expires-in 30d
.\bin\fakeapi.exe serve --config .\runtime\preset\fakeapi.yaml
```

使用发行包时，在解压目录执行：省略 `go build`，把命令中的
`.\bin\fakeapi.exe` 改为 `.\fakeapi.exe`。发行包启动不需要 Go、Python 或 Node。

保存创建命令显示的 Key，再填入客户端。服务没有固定默认 Key。Key 原文仅显示一次，数据库保存摘要。

Linux/macOS 用 `go build -o bin/fakeapi ./cmd/fakeapi`，后续子命令相同。

| 接口 | 客户端基址 | 生成路径 |
|---|---|---|
| OpenAI Chat | `http://127.0.0.1:8787/v1` | `/v1/chat/completions` |
| OpenAI Responses | 同上 | `/v1/responses` |
| Anthropic | `http://127.0.0.1:8787/anthropic` | `/anthropic/v1/messages` |
| Gemini v1beta | `http://127.0.0.1:8787` | `/v1beta/models/{id}:generateContent` 或 `:streamGenerateContent?alt=sse` |

展示模型默认是 `fakeapi-frog`（自然节奏）与 `fakeapi-test`（极速）。模型名称和 ID 均可在 YAML 修改。Anthropic 需要 `anthropic-version: 2023-06-01`；Gemini 接受 `x-goog-api-key`。

## 默认行为

- 问候、身份、简单四则、指定测试题有本地预设。
- 鹈鹕骑自行车 SVG 测试有独立静态 fixture，混合模式可以交给真实 AI。
- `/frog`、`/laugh`、`/wave`、`哈哈哈`、`奶蛙思考` 等触发彩蛋。
- 其他内容随机返回四个指定形象：挥手、站立、捧腹大笑、思考。
- 文案围绕大笑、捧腹、思考和绷不住；正常问候保持自然。

`/test/json` 和 `/test/tool` fixture 使用 `fakeapi-test` 模型及 `test` policy 创建的 Key。

[字符画预览](docs/ascii-preview.html) · [素材来源](docs/ASSET_NOTICES.md)

匹配仅看最新语义用户消息。最新消息只有图片时，不回看历史文本。命中真实 AI 规则时，上游收到完整的受支持上下文。

## 混合模式

```powershell
.\bin\fakeapi.exe init --template hybrid --dir .\runtime\hybrid
$env:FAKEAPI_UPSTREAM_BASE_URL = 'https://your-provider.example/v1'
$env:FAKEAPI_UPSTREAM_API_KEY = 'replace-with-your-upstream-key'
$env:FAKEAPI_UPSTREAM_MODEL = 'your-model'
$env:FAKEAPI_UPSTREAM_SVG_MODEL = 'your-svg-model'
.\bin\fakeapi.exe config check --config .\runtime\hybrid\fakeapi.yaml
.\bin\fakeapi.exe keys create --config .\runtime\hybrid\fakeapi.yaml --policy hybrid --label hybrid-local
.\bin\fakeapi.exe serve --config .\runtime\hybrid\fakeapi.yaml
```

混合模板只对带 `ai` 标签的 Key 启用问候、身份、测试题和鹈鹕四类上游规则。规则可独立关闭。展示模型、规则和默认目标可映射多个服务/真实模型。第一版上游采用 OpenAI Chat 兼容协议。

离线开发时可先运行 `go run ./tests/mockupstream/cmd --listen 127.0.0.1:9099`，把四个环境变量改为本地地址、任意测试上游凭据、`mock-fast`、`mock-svg`。本程序不会自动加载 `.env`。

## 配置与管理

```text
fakeapi config check --config FILE [--skip-env]
fakeapi config show --config FILE --redact
fakeapi config schema
fakeapi keys list --config FILE
fakeapi keys show --config FILE --id ID
fakeapi keys update --config FILE --id ID --ai-limit 100 --rpm 60
fakeapi keys revoke --config FILE --id ID
fakeapi keys reset-usage --config FILE --id ID --confirm
fakeapi assets list --config FILE
fakeapi assets import --config FILE --id custom --kind ascii --author NAME --license LICENSE ./custom.txt
fakeapi rules explain --config FILE --input ./request.json --protocol chat --model fakeapi-test
fakeapi db backup --config FILE --out ./backup.sqlite
fakeapi health --url http://127.0.0.1:8787
```

参数放在位置参数之前。时间支持 `30d/12h`，到期日期用带时区的 RFC3339。额度 `0` 表示禁止，`unlimited` 表示无限。Key policy 是创建/显式更新模板，修改 YAML 不自动修改已有 Key。

YAML 严格校验未知字段、重复键、引用和环境变量。路径相对配置文件目录。配置、规则、素材是启动快照，修改后重启。详见 [配置](docs/configuration.md) 和 [额度](docs/quotas.md)。

## Docker

```powershell
docker compose build
New-Item -ItemType Directory -Force .\runtime | Out-Null
$runtimePath = (Resolve-Path .\runtime).Path
docker run --rm --user 0:0 --mount "type=bind,source=$runtimePath,target=/config" fakeapi:local init --template preset --shared-config --dir /config
docker compose run --rm --no-deps fakeapi config check
docker compose run --rm --no-deps fakeapi keys create --policy demo --label docker-local
docker compose up -d
```

默认绑定宿主机 `127.0.0.1:8787`。容器以非 root 用户运行，配置只读，SQLite 使用独立数据卷。升级保留数据卷，先备份数据库。部署流程见 [部署与升级](docs/deployment.md)。

`--shared-config` 只让初始化的 YAML 和素材可被服务容器读取，文件不可由其他用户写入；
不生成实际 Key 或数据库。普通本地 `init` 继续采用私有文件权限。

## 测试

```text
go test ./...
go vet ./...
python -m pip install -r tests/sdk/python/requirements.txt
npm ci --prefix tests/sdk/node
go build -o bin/fakeapi.exe ./cmd/fakeapi
python scripts/test-sdk.py
```

Linux/macOS 构建文件名用 `bin/fakeapi`。SDK 脚本自动启动临时本地服务、创建临时 Key、执行断言并清理。测试不需要真实 AI 或付费 Key。CI 在 Linux、Windows 和 macOS 运行 race detector。

`python scripts/runtime-smoke.py` 验收已构建二进制的实际运行；
`python scripts/runtime-smoke.py --docker --image fakeapi:ci` 验收 Compose 部署。
GitHub Actions 同时验收三系统原生运行及解压后的发行包，报告和归档可在
[Actions](https://github.com/CyrusAuyeung/FakeAPI/actions) 下载。详见 [自动运行验收](docs/acceptance.md)。
跨版本升级与备份恢复使用 `scripts/upgrade-smoke.py`；
`scripts/measure-performance.py` 提供 [性能观察](docs/performance.md)，没有固定速度门槛。

## 范围与数据

服务记录的请求日志恰好四个字段：`time/model/duration_ms/rule`。不保存请求正文、回复、图片、推理、IP 或 Key 原文。额度统计获准执行的请求，失败和取消也保留已扣次数；模型发现和健康检查不扣生成额度。

支持有限 JSON/工具 fixture；服务不执行工具。Responses 采用客户端重传上下文的无状态模式。原生签名思考、有状态 Responses、音视频生成、多候选和完整 Agent 循环属于后续范围。[协议范围](docs/protocol-matrix.md) 列出具体限制。

软件代码使用 MIT；每份素材的来源和许可独立登记。第三方许可见 [声明](docs/THIRD_PARTY_NOTICES.md)。
