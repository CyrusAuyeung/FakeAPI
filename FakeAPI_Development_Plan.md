# FakeAPI 开发计划

**设计版本：1.0｜协议核对日期：2026-10-12｜实施目录：`D:\Projects\FakeAPI`｜许可证：MIT**

建议将本文放入实施仓库的 `docs/development-plan.md`，作为 Codex 连续实施的主计划。本文提供设计、配置和协议契约，不包含完整项目实现。文中的客户端“完整支持”均指 **v1 发布必须达到的指定能力及验收目标**；目前没有 FakeAPI 实现或客户端联调通过记录。

**阅读导航：** [范围](#1-项目目标与第一版范围) · [选型](#2-技术选型与依赖) · [架构](#3-架构目录与核心数据结构) · [匹配与流程](#4-请求流程与规则语义) · [协议矩阵](#5-协议兼容矩阵与关键映射) · [配置与 CLI](#6-配置素材命令行和模型路由) · [节奏与额度](#7-节奏流式失败工具状态与请求计数) · [实施阶段](#8-分阶段实施任务) · [测试](#9-测试方案和兼容验收) · [部署发布](#10-部署开源发布升级和风险)。文末附完整纯预设/混合 YAML、素材清单、协议请求和 54 个官方资料链接。

## 1. 项目目标与第一版范围

### 1.1 最终推荐

采用 **Go 单进程 + 标准库 HTTP/SSE + YAML 配置 + SQLite 本地状态**。项目自身实现协议适配与有限规则引擎，运行时不引入 AI 代理框架，也不安装各供应商 SDK。

面向客户端分阶段实现 OpenAI Chat Completions、OpenAI Responses、Anthropic Messages、Gemini Generate Content；v1 完成四者的**文本生成核心、普通/流式响应、模型目录、错误和有界测试场景**。真实 AI 上游第一版只实现 **OpenAI Chat Completions 兼容适配器**。四种入站协议不等于四种原生上游。

默认自然节奏，预设模拟思考开启；默认通过明确标记的普通文本显示，确保不依赖客户端私有思考面板。测试模型、JSON、工具、精确答案和 SVG 场景使用极速节奏并关闭普通文本思考前缀。AI 分支不添加模拟思考，不主动改写用户原请求。

### 1.2 v1 必做

- 一个可执行文件，一份配置，一个本地 SQLite 文件；Docker 为等价部署方式。
- 四种协议的文本核心与确定的流式生命周期；单次生成只产生一个 choice/candidate。
- 最新语义用户消息匹配；精确、关键词、正则、优先级、启停、固定/随机内容、模型和 Key 元数据条件。
- 纯预设配置和混合配置；四类 AI 分流规则可独立开关；多上游地址、多真实模型和明确映射。
- 三种节奏模板、可取消的等待、响应时限、可复现的随机选择和节奏。
- 原创奶蛙 ASCII 素材、文本/Markdown 导入、许可记录。
- 单函数调用及一次结果回传的显式 fixture；固定 JSON/有限 JSON Schema fixture；不执行工具。
- Key 创建/查看/撤销/到期/请求额度/RPM/并发限制，重启保留额度和当前速率窗口。
- 严格四字段请求日志；中文 README、英文说明、离线测试、SDK 契约测试、GitHub Actions 和镜像发布。

### 1.3 明确暂缓

Web 界面、注册和计费平台、Redis、外部数据库、多实例共享额度、消息/响应持久化、通用插件语言、AI 意图分类、无限 Agent 循环、工具执行、自动模型发现/供应商探测、自动跨供应商故障切换。

协议扩展暂缓：Responses `previous_response_id`/Conversations/存储查询/后台任务/取消后台任务/WebSocket/内置工具；Anthropic Files、原生签名思考生成、复杂 beta 与服务器工具；Gemini Interactions、Live、Files 上传、缓存、grounding、音视频生成；多个候选、完整 logprobs、任意 JSON Schema 自动生成。

### 1.4 合理假设及发布顺序

| 项目 | 约定 |
|---|---|
| 部署 | 一台机器上一个服务进程，SQLite 位于本地磁盘；CLI 可与服务并发管理同一数据库 |
| 对话 | 每次请求由客户端携带所需完整上下文；服务不保存会话 |
| 模型 | 展示 ID 完全由配置定义；不自动冒充真实供应商模型能力 |
| 素材 | 奶蛙按原创“奶白色小青蛙”主题制作；不直接抓取来源不明的表情包 |
| 真实模型 | 部署者提供服务地址、模型 ID 和环境变量密钥；模型能力需显式声明 |
| CI | 锁定依赖安装完成后，测试仅访问本机模拟上游；不需要付费 AI |
| 发布顺序 | v0.1 纯预设 Chat → v0.2 混合 Chat → v0.3 Responses → v0.4 Messages → v0.5 Gemini → v1.0 完成全部发布验收 |

阶段版本可以提前供试用，但 README 必须只列该版本已经完成的协议。预估 24–36 个工程工作日，用于排期参考；以阶段验收结果推进，不把 Codex 生成速度当作确定工期。

## 2. 技术选型与依赖

### 2.1 方案比较

| 维度 | Python + FastAPI | TypeScript + Node.js | Go + net/http |
|---|---|---|---|
| 四种协议/联合类型 | Pydantic 建模便利，需谨慎避免框架默认错误形状 | 类型表达好，SDK 生态齐全 | 需要更多显式类型、RawMessage 和转换代码 |
| SSE/取消 | async generator 可用，需处理断连与 task 清理 | async iterator、AbortController 自然 | context、HTTP transport、goroutine 适合统一取消 |
| 配置/规则 | 实现快，生态丰富 | Schema 工具丰富 | 强类型配置 + 标准库 RE2，边界容易固定 |
| 本地部署 | 需 Python/虚拟环境或独立打包 | 需 Node 或单独打包 | 用户直接运行对应平台二进制 |
| 单人维护 | 业务迭代快，运行环境与依赖需维护 | 依赖树、构建与类型版本需维护 | 代码略多，运行层较简单 |
| 启动与资源 | 需实测 | 需实测 | 适合小服务，但 SQLite 依赖会增加二进制体积；仍需实测 |
| 测试/发布 | pytest 好用，跨平台分发较繁琐 | JS 测试好用，运行时打包需安排 | go test、交叉编译和标准库测试服务便利 |
| 贡献门槛 | AI 开发者通常熟悉 | 前端/全栈开发者熟悉 | 需读 Go，但模块少、协议代码直接 |

**选择 Go 的主要理由是部署和维护目标。** 不为追求极端性能引入复杂框架，也不声称它在所有情况下都优于 Python/Node。

### 2.2 具体选择

构建基线采用 Go 1.27.2；P0 再检查补丁更新并将实际版本固定到 CI、Docker build arg 和工具链文件。官方当前下载页列出了该稳定版本。[T1]

| 组件 | 推荐 | 用途和许可 |
|---|---|---|
| HTTP/JSON/CLI/日志/正则 | Go 标准库 | net/http、encoding/json、flag、slog、regexp；不引入 Gin/Cobra/ORM |
| YAML | `go.yaml.in/yaml/v3`，起始评估 v3.0.5 | 强类型加载、行号错误；MIT/Apache-2.0，保留相应声明 |
| SQLite driver | `modernc.org/sqlite` | 免 CGo，方便跨平台构建；BSD-3-Clause，SQLite 本体 public domain，并保留传递依赖许可 |
| JSON Schema | `github.com/santhosh-tekuri/jsonschema/v6` | 校验固定 JSON/工具参数；Apache-2.0；禁用网络和文件外部引用加载 |
| 字符分块/宽度 | `github.com/rivo/uniseg` | 按 grapheme cluster 分块、宽度检查；MIT |
| 单实例文件锁 | `github.com/gofrs/flock` | 服务持有 `state.sqlite.lock`，阻止同库第二服务进程；BSD-3-Clause；CLI 不取此服务锁 |
| SDK | OpenAI、Anthropic、Google 官方 Python/TS SDK | 仅开发测试依赖；OpenAI/Google 为 Apache-2.0，Anthropic 为 MIT，P0 核验锁定版本的 LICENSE |

YAML 原项目已经停止维护，YAML 组织维护了新分支；本次读取到 v4 仍为 release candidate，因此 v1 使用安全维护中的稳定 v3。等 v4 稳定后单独迁移，不在开发中途随意变更配置解析行为。[T2][T3] 这些是少量直接依赖，**不等于零传递依赖**；版本和全部传递许可须通过 `go.mod/go.sum` 与第三方许可清单锁定。[T4][T5][T6][T7]

项目自主实现路由、状态机和协议编码。可参考官方 SDK 的事件累积与测试思路，默认不复制现有网关代码。新增依赖必须说明它替代了什么复杂工作，并在许可清单中登记。

## 3. 架构、目录与核心数据结构

### 3.1 模块职责

| 模块 | 负责 | 不负责 |
|---|---|---|
| `config` | YAML、默认值、引用校验、环境变量引用、编译配置快照 | 不调用上游，不求值脚本 |
| `protocol` | 入站解析、字段策略、模型列表、响应与错误编码、流事件编码 | 不决定业务规则、额度和内容 |
| `core` | 协议无关请求/内容/事件/错误类型 | 不依赖 HTTP handler 或 SQLite |
| `auth` | 凭据提取、摘要验证、Key 元数据 | 不信任客户端自报标签 |
| `store` / `quota` | SQLite 迁移、Key 管理、原子准入、持久计数 | 不在事务内等待网络 |
| `rules` | 最新用户文本、有限条件、优先级、选中一个动作 | 不读取系统/历史/工具文本，不调用 AI |
| `preset` | 固定/随机内容、有限算术、JSON 和单工具 fixture | 不执行工具、不泛化回答任意题目 |
| `assets` | UTF-8 文件、来源清单、导入、宽度/换行校验 | 不进行图片转 ASCII |
| `pacing` | 预设思考、分块、延迟、停顿、可取消定时器 | 不二次限速真实上游 |
| `upstream` | 目标解析、参数转换、HTTP/SSE 读取、能力检查、超时 | 不重新判断意图、不自动重试/切换供应商 |
| `server` | 生命周期、准入编排、并发位、协议路由、响应提交边界 | 不包含匹配表达式和素材内容 |
| `requestlog` | 在结束时写唯一四字段事件 | 不记录通用 request/response 对象 |
| `cli` | 本地初始化、检查、Key/素材管理和 dry-run | 不提供远程管理 API |

### 3.2 建议目录

```text
FakeAPI/
  cmd/fakeapi/main.go
  internal/
    cli/
    config/                 # types.go load.go validate.go schema.go
    core/                   # request.go content.go event.go error.go
    server/                 # routes.go pipeline.go lifecycle.go
    protocol/
      openaichat/           # request response stream errors models
      openairesponses/
      anthropic/
      gemini/
      fieldpolicy/
    auth/
    store/sqlite/           # migrations/ repository.go
    quota/
    rules/                  # extract.go compile.go match.go explain.go
    preset/                 # render.go arithmetic.go json.go toolfixture.go
    pacing/                 # profiles.go chunk.go clock.go rng.go
    upstream/
      openaichat/           # translate.go client.go sse.go
      capabilities.go
    assets/
    requestlog/
    buildinfo/
  assets/
    embed.go                # 仅导出默认素材 embed.FS
    manifest.yaml
    milkfrog/               # idle.txt hello.txt thinking.txt milk.txt shrug.txt party.txt
    text/                   # egg-milk.txt egg-croak.txt egg-afterwork.txt egg-party.txt
    thinking/               # natural.txt playful.txt
    fixtures/               # pelican-bicycle.svg.txt json-ok.json weather-tool.json
  examples/
    embed.go                # 仅导出指定配置模板；init 的 env.example 从引用生成
    preset/fakeapi.yaml
    hybrid/fakeapi.yaml
    testing/fakeapi.yaml
    requests/               # 四种协议的人工请求，不含真实用户记录
  schemas/config.v1.schema.json
  tests/
    contract/               # request/response/SSE fixtures 与快照
    integration/
    privacy/
    sdk/python/             # pyproject.toml uv.lock
    sdk/node/               # package.json package-lock.json
    mockupstream/           # controllable HTTP server + cmd/
    clients/                # 手工验收脚本与结果模板
  docs/
    development-plan.md
    architecture.md
    configuration.md
    protocol-matrix.md
    compatibility.md
    quotas.md
    assets.md
    deployment.md
    upgrade.md
    THIRD_PARTY_NOTICES.md
    decisions/
  scripts/                  # Windows PowerShell 和 POSIX 测试/发布辅助
  .github/workflows/        # ci.yml sdk.yml release.yml
  .github/ISSUE_TEMPLATE/
  Dockerfile
  compose.yaml
  .env.example
  .gitignore
  .dockerignore
  README.md
  README.en.md
  CONTRIBUTING.md
  SECURITY.md
  CHANGELOG.md
  LICENSE
  go.mod
  go.sum
```

`data/`、`runtime/`、本地 `fakeapi.yaml`、`.env` 和输出二进制放入 `.gitignore`。示例和默认素材通过各自根目录的 `embed.go` 随二进制分发，`init` 将其导出为用户可编辑文件；internal 模块导入这两个只导出数据的包，不能使用 Go 不支持的 `go:embed ../../assets`。只嵌入明确的发布文件/目录，不包括用户 imports、真实 .env 或运行数据。默认素材不在二进制与仓库中维护两份手工副本。

### 3.3 协议无关数据结构

下表是类型契约，不要求现在写出全部 Go 代码。所有可选布尔/整数必须区分“未提供”与零值；JSON 数字使用 `json.Number` 或明确的整数/浮点验证。

| 类型 | 关键字段 |
|---|---|
| `NormalizedRequest` | `protocol`, `model_id`, `stream`, `conversation[]`, `latest_user`, `tools[]`, `tool_choice`, `output_constraint`, `parameters`, `protocol_extras` |
| `Message` | `role`（system/developer/user/assistant/tool）, `parts[]`, `source_index`；保留原顺序及来源，不先合并同角色消息 |
| `Part` | 联合类型：text/image/tool_call/tool_result/reasoning；对应 `text`, `url\|base64+mime`, `call_id/name/arguments`, `opaque_metadata` |
| `LatestUser` | `present`, `source_index`, `text`, `has_images`；图片-only 是 present=true、text="" |
| `MatchInput` | **仅** latest_user 的文本/存在性及 model_id、可信 Key tags/group；不持有完整 conversation 指针 |
| `KeyContext` | `key_id`, `label`, `tags[]`, `group`, `expires_at`, `limits`；不进入日志 |
| `RouteDecision` | `rule_id`, `action`, `target_id?`, `effective_profile`, `on_error` |
| `OutputPlan` | `kind:text\|ascii\|svg\|json\|tool_fixture`, `thinking`, `body/asset/json`, `tool_spec?`, `schedule`, `estimated_usage` |
| `Event` | `kind:start\|reasoning_delta\|text_delta\|tool_start\|tool_args_delta\|tool_done\|usage\|finish\|error`，`index`, `text`, `tool`, `usage`, `finish`, `error` |
| `Usage` | `input`, `visible_output`, `reasoning_output`, `cached_input?`, `source:estimated\|upstream\|mixed`；visible 与 reasoning 不重叠 |
| `PublicError` | `code`, `http_status`, 固定安全 `message`, `parameter?`, `retry_after?`；不包含上游原始正文或带密钥 URL |
| `Admission` | `key_id`, `is_ai`, `committed`；进程内一次性对象，不是持久请求账本 |

`protocol_extras` 只在请求生命周期内保存用于校验或受支持的保留字段。发现未知的功能性内容块，必须报错或明确声明为可忽略字段；不得把 RawMessage 通用透传当成跨协议转换。

### 3.4 关键接口边界

```text
Decode(protocol, request) -> NormalizedRequest | PublicError
ExtractLatestUser(conversation, protocol) -> LatestUser
Match(compiledRules, MatchInput) -> RouteDecision
PreparePreset(request, decision) -> OutputPlan | PublicError
ResolveTarget(request, decision, config) -> UpstreamTarget | PublicError
TranslateToUpstream(request, target) -> UpstreamRequest | PublicError
AcquireCapacity(context, key, route) -> ReleaseFunction | PublicError
Admit(context, keyID, isAI, now) -> Admission | PublicError
RunPreset(context, plan, emit) -> error
RunUpstream(context, upstreamRequest, emit) -> error
EncodeEvent(protocol, state, event) -> wire events | PublicError
WriteRequestLog(time, verifiedModel, duration, ruleID)
```

协议编码器负责从通用事件构造各自状态对象，特别是 Responses 的最终 output 和 Anthropic 的 content 下标。避免把某一供应商响应对象当成所有协议的内部数据模型。

## 4. 请求流程与规则语义

### 4.1 完整处理流程

1. 路由到协议入口，建立请求 context，限制 body/header 大小与读取时间；只接受 JSON。
2. 提取相应协议凭据并查验 Key，解析字段和展示模型；错误以相应协议编码。
3. 规范化完整上下文；提取最新语义用户消息，生成独立的 `MatchInput`。
4. 规则按优先级选择一个动作；未命中进入唯一全局 fallback。
5. 预设分支加载已校验素材/fixture并验证输出约束；AI 分支解析目标、转换完整上下文并检查能力。
6. 非阻塞取得服务、Key 和必要的上游并发位；拿不到返回明确错误，释放已经取得的位，不排无限队列。
7. SQLite 短事务再次验证 Key 撤销/到期、RPM、总额度和 AI 额度，并一次性消费对应计数。
8. 执行预设计划或上游请求。流式输出在第一个有效待发送事件准备好之后才提交 HTTP 200/SSE。
9. 根据协议输出完整响应或事件流。结束、失败、取消都走同一清理路径。
10. 释放 response body、定时器、goroutine 和并发位；写一次四字段请求日志。

模型发现需要有效 Key，但不消耗生成次数；Key 总额度为零仍可以查看模型。`/healthz` 和 `/readyz` 无需 Key、不记生成请求日志。

### 4.2 最新用户消息的精确定义

| 协议 | 最新消息选择和文字提取 |
|---|---|
| Chat Completions | 从 `messages` 尾部寻找最后一个 `role:user`；字符串 content 直接取，数组仅取顶层 `type:text` 的 text |
| Responses | `input` 字符串视为一条 user；数组从后向前找 user message（含省略 type 的 easy message）；只取普通文本/`input_text`；instructions、function_call_output、reasoning item 不参与 |
| Anthropic | 顶层 system 排除；`role:user` 且**非空且所有块都是 tool_result** 的包装消息视为工具消息而跳过；选择最后一个其他 user，取字符串或顶层 text，不取 tool_result.content 内的文本 |
| Gemini | systemInstruction 排除；`role:user` 且非空 parts 全为 functionResponse 的包装视为工具消息；取最后一个其他 user Content 中普通 text，排除 thought:true；model 角色等价 assistant |

共同规定：

- 文本块按原顺序使用一个 `\n` 连接；图片只留下 `has_images`，不 OCR、不获取 URL、不把 base64/alt/文件名拼入文字。
- 最新图片-only 或合法的空文本消息仍是最新用户消息，其匹配文本为空；不能回退到旧的有文字消息。
- 消息为空或块类型不合法时，先按协议契约返回 400；不能为了让规则命中而跳过坏消息。
- 若没有语义用户消息，`present=false`、text=""；所有普通文本条件不匹配，走 fallback 或显式元数据规则。允许的 system-only 请求也不拿 system 代替用户。
- Gemini 单个 Content 缺 role 可视为 user；多轮缺 role 默认拒绝，避免猜测。SDK 在发包前如何合并输入，以服务器收到的最终结构为准。
- 不合并相邻 user 后再匹配；例如先前的“你好”和最新“帮我写报告”不能合成一个候选。

Anthropic 和 Gemini 用 user 包装工具结果，是必须做“语义消息”区分的原因；这些包装不会变成新的文本路由来源。[A3][G4]

### 4.3 文本规范化和条件组合

文本规范化固定为 CRLF/CR→LF、去除首尾 Unicode 空白。保留中间空格、换行、标点和全角字符，不做 NFKC，不自动去除句子中的词。`max_chars` 按规范化后的 Unicode code point 数计数；它与按 grapheme 进行的输出分块是两件事。

条件分为两层：

1. `matchers` 定义只针对当前文本的 `any`、`all`、`none`，原子项为 `exact`、`keyword` 或 `regex`，可加 `max_chars`。
2. 每条 rule 的 `when` 将 `matchers_any`（引用任一文本 matcher）与 `models`、`key_tags_any`、`key_tags_all`、`key_groups` 等元数据条件做 AND。

不支持任意递归逻辑、脚本、数据库查询、网络条件或用户自定义函数。每个 matcher 最多 32 个原子项，每条正则最多 2 KiB、匹配输入受请求上限保护。Go RE2 不支持回溯引用和 lookbehind，应在配置检查时给出明确错误。

`exact` 对规范化后的整条文本比较；`keyword` 默认区分大小写的子串，可显式 `case_insensitive:true`，可选 `boundary:ascii_word`；中文没有隐式分词。`regex` 默认按 RE2 查找，整句匹配必须写 `^...$`；需要不区分大小写写 `(?i)`。

`any` 缺省或为空表示不增加 OR 限制，`all` 必须全部命中，`none` 必须全部不命中；至少一个正向文本条件才是普通 matcher。空文字不会命中普通 matcher，即使某条正则可以匹配空串。无文本行为使用显式 `when.user_text:absent|empty`，不能靠 `.*` 隐式捕获。

### 4.4 优先级与默认规则

规则按 `priority` 数值降序排列，同优先级按配置数组顺序；先命中即停止。重复 ID 为错误，同优先级重叠给 lint 提示。`enabled:false` 完全跳过。

全局 `fallback` 独立于普通优先级，必须存在且不能关闭。它默认随机选择奶蛙素材。AI 失败后的备用行为由已经命中的 AI 规则决定，**不会重新从规则列表开始匹配**。

建议默认规则：

| 类别 | 明确的匹配范围 | 纯预设动作 |
|---|---|---|
| 问候 | 最多 24 字，整句 `你好/您好/hi/hello/hey` 加可选句末标点 | 两条固定问候随机选择 |
| 身份 | “你是什么模型”“你是谁”“what model are you”等完整短句 | 明示 FakeAPI 模拟服务和展示 model ID |
| 简单数学 | 最多 40 字，`[-]整数 运算符 [-]整数`，每个整数 1–6 位，运算符 + - * /，可带“计算”与句末 =? | 有限算术处理器，不使用 eval；加减乘精确整数，除法用约分有理数，除零返回固定文字 |
| 小数题 | 配置中列出的“9.9和9.11哪个大”等完整问法 | 固定答案 9.9 更大 |
| 字母计数 | 明列 strawberry 中 r 数量的完整问法 | 固定答案 3 |
| 连通性测试 | “只回答 OK”等完整指令 | 严格输出 `OK`，关闭思考 |
| 鹈鹕测试 | “用 SVG 画骑自行车的鹈鹕”及少量明确的英/中文等价表达 | 原创静态 SVG fixture 文本，非任意绘图器 |
| 彩蛋 | `/frog`、`/milk`、`呱呱`、`奶蛙下班` 等具体规则 | 对应奶蛙素材/短文案 |
| 其他 | fallback | 奶蛙 ASCII 或预设内容 |

问候、身份和数学 matcher 默认锚定整句；“你好，请分析以下 200 行代码”应落到普通 fallback，不当作简单问候。鹈鹕 matcher 也使用整句/限定模板；包含“SVG/自行车/鹈鹕”但任务不同的长文不自动分流。用户可在 YAML 中增添具体别名，而不是宣称服务能识别所有测试题。

## 5. 协议兼容矩阵与关键映射

### 5.1 路由和 SDK 基址

统一监听 `127.0.0.1:8787`。容器内监听 `0.0.0.0:8787`，默认只向宿主机 127.0.0.1 发布端口。

| 协议 | 实际路由 | SDK 配置基址 |
|---|---|---|
| OpenAI Chat | POST `/v1/chat/completions`；GET `/v1/models`、`/v1/models/{id}` | `http://127.0.0.1:8787/v1` |
| OpenAI Responses | POST `/v1/responses`；共用 OpenAI 模型目录 | 同上 |
| Anthropic | POST `/anthropic/v1/messages`；GET `/anthropic/v1/models`、`/anthropic/v1/models/{id}` | `http://127.0.0.1:8787/anthropic`，不追加 `/v1` |
| Gemini | POST `/v1beta/models/{id}:generateContent`、`/v1beta/models/{id}:streamGenerateContent?alt=sse`；GET `/v1beta/models` 和 `/v1beta/models/{id}` | `http://127.0.0.1:8787`，明确 `api_version/apiVersion=v1beta` |

可额外提供 POST `/v1/messages` 别名，供只需生成的客户端使用；模型发现仍以 Anthropic 命名空间为准。根路径 `/v1/models` **固定是 OpenAI 格式**，不依据 Key header 猜协议。Gemini v1 若后续实现，采用 `/gemini/v1` 等独立命名空间。

### 5.2 v1 能力矩阵

标记：**完整**＝该行限定能力的发布门槛；**部分**＝存在明确条件/语义差异；**暂缓**＝v1 不承诺。兼容结论必须最终写入锁定版本的测试结果。

| 能力 | Chat Completions | Responses | Anthropic Messages | Gemini Generate Content |
|---|---|---|---|---|
| 自定义模型、目录 | 完整 | 完整，共用目录 | 完整，独立目录 | 完整，仅 v1beta |
| 单候选文本非流式 | 完整 | 完整，无状态 | 完整 | 完整 |
| 单候选文本流 | 完整 SSE | 完整事件生命周期 | 完整事件生命周期 | 完整 `alt=sse` |
| 完整文本上下文接收 | 完整 | 完整，客户端重传历史 | 完整，顶层 system | 完整，contents + systemInstruction |
| SDK 文本累积/结束 | 完整 | 完整，包括最终 output | 完整，包括最终 content | 完整，文本增量与 finishReason |
| Key/错误/HTTP 状态 | 完整，本计划映射 | 完整，本计划映射 | 完整，本计划映射 | 完整，本计划映射 |
| usage | 完整结构；预设是估算 | 同左 | 同左；跨协议流式有估算限制 | 同左；thoughts 不重复计入 |
| 输出上限/停止序列 | 完整模拟语义 | max_output_tokens；无自创 stop 字段 | max_tokens/stop_sequences | maxOutputTokens/stopSequences |
| 默认模拟思考 | 完整，普通文本标注 | 完整，普通文本标注 | 完整，普通文本标注 | 完整，普通文本标注 |
| 原生/扩展思考面板 | 部分，reasoning_content 非官方通用字段 | 部分，原生 summary 事件 | 暂缓签名 thinking；普通文本可用 | 部分，thought part 无 Google 签名 |
| 图片 + 文字请求 | 完整接收，预设不理解图片；AI 受能力限制 | 同左 | 同左 | 同左 |
| 仅图片请求 | 完整解析/空文字 fallback | 同左 | 同左 | 同左 |
| 普通函数声明 | 完整接收；auto 可返回文本 | 同左 | 同左 | 同左 |
| 工具 fixture | 完整：单调用 + 一次回传 | 同左 | 同左 | 单 functionCall 整块发送，不模拟参数增量 |
| 任意工具/并行/内置工具 | 暂缓完整保证 | 暂缓 | 暂缓 | 暂缓 |
| 固定 JSON/Schema fixture | 完整：声明的有限子集 | 同左，text.format | 同左，output_config.format | 部分新旧字段归一化；限定 Schema 子集 |
| 任意 Schema 自动生成 | 暂缓 | 暂缓 | 暂缓 | 暂缓 |
| temperature/top_p 等预设采样 | 接收、校验；不等于真实采样语义 | 同左 | 同左 | 同左 |
| 客户端取消服务资源 | 完整 | 完整 | 完整 | 完整，需客户端实际 abort/close |
| 中途错误被所有 UI 显示 | 部分 | 部分 | 部分 | 部分，SDK 解析差异须实测 |
| 存储/后台/会话引用 | 暂缓 | 暂缓 | 暂缓 | 暂缓 |

四类协议都在发展；Gemini 当前将 Generate Content 单独列为 Legacy，主要新指南开始转向 Interactions。FakeAPI 应继续实现用户指定的 Generate Content，不能将新接口字段和事件混入旧接口。[G1][G2]

### 5.3 请求字段策略

为每种协议维护一个可测试的字段策略表，区分 `honor`、`accept_without_effect_for_preset`、`translate_if_capable`、`reject`。通用 Decoder 不应直接返回框架默认 422。

**共同规则：**已知无行为影响的提示性字段可白名单接受；未知顶层字段/未知功能性内容块默认返回 400 `unsupported_parameter`，message 说明字段路径与支持范围，不回显字段值。禁止默默丢弃图片、工具结果、系统指令或强制输出约束。

| 协议 | v1 必须处理的主要字段 | 明确的部分/暂缓字段 |
|---|---|---|
| Chat | model/messages/stream、content 字符串与 text/image_url、system/developer/user/assistant/tool、tools/tool_choice、max_tokens 或 max_completion_tokens、stop、response_format、stream_options.include_usage | n 仅 1；logprobs/音频/旧 functions 暂缓；两个 token 上限同时非空返回 400；store 仅 false/省略 |
| Responses | model、input 字符串/数组、instructions、role/message items、input_text/input_image、function_call/function_call_output、stream、max_output_tokens、tools/tool_choice、text.format、store:false | previous_response_id/conversation/background/store:true、item_reference、内置工具、compact/encrypted reasoning 处理暂缓 |
| Anthropic | model/messages/max_tokens/stream、顶层 system、text/image、tool_use/tool_result、tools/tool_choice、stop_sequences、output_config.format、thinking 的关闭/显示意图 | max_tokens:0 缓存预填暂缓；消息级 system、复杂 cache_control/context_management/citations/beta 工具暂缓 |
| Gemini | contents、systemInstruction、parts.text/inlineData/fileData/functionCall/functionResponse、tools、toolConfig、generationConfig 的候选数/上限/stop/采样/JSON/thinking 显示开关 | candidateCount 仅 1；cachedContent、安全设置效果、grounding、Google 文件服务、内置执行、音视频暂缓 |

`temperature/top_p` 和相关采样参数在预设分支只做类型/范围验证；随机内容由 FakeAPI 种子系统控制，不让 temperature 意外改变规则。`metadata/user/safety_identifier` 等可接受的辅助字段，逐项列入白名单，不持久化；向上游传递也需要目标能力允许。

Responses 无状态续聊必须接收前次 response.output 中可重放的 assistant message/output_text、function_call 等 item，并允许其中的 id/status/annotations 等已知字段。不能只支持首次 user input_text、却用 unknown-field 错误拒绝 SDK 的手动历史重传。reasoning 的 effort/summary 显示意图可解析；encrypted_content/item_reference 的真实回放能力仍暂缓，不能把它们当普通文字删除。[O4]

普通预设遇到图片、工具声明且工具选择为 auto/none 时仍返回文本/字符画。**若请求强制 JSON、Schema 或指定工具，而选中的预设无法满足约束，默认 400 `unsupported_constraint`**；不能返回成功状态却输出违反约束的奶蛙。显式 JSON/工具 fixture 或具备能力的 AI 分支承担这些测试。此选择保持普通预设简单，也使兼容结果可解释。

JSON Schema 子集限定为 object/array/string/number/integer/boolean/null、properties/required/additionalProperties、items、enum/const、基本数值/长度边界；可支持本地 `$defs/$ref`，深度上限 16，拒绝外部 `$ref`、递归引用和无限扩张结构。遇到未支持关键词返回 400，不假装验证了它。Gemini 的旧 schema 枚举类型可做明确的类型名转换，nullable 映射到允许 null；OpenAPI 特有含义不能直接当成完整 JSON Schema。

另列已知注解白名单 title/description/default/examples；仅接受受支持版本的 `$schema` 声明，不因此启用网络加载，不将 default 自动写入 fixture。允许 type 的有限联合及仅用于 nullable 的 anyOf；更复杂组合暂缓。Gemini `propertyOrdering` 验证为无重复的已知 properties 名称列表，作为顺序提示接受，v1 保证 JSON 语义与类型解析，不承诺字节级键顺序；跨 Chat 时可按该明确白名单去掉。Google SDK 会自动生成此提示，必须用带两个字段和 description 的 Pydantic 模型验收，不能只测手写极简 JSON。[G10]

当前 Anthropic 结构化输出使用 `output_config.format`。Gemini 已出现 `generationConfig.responseFormat.text`，同时存在旧 `responseMimeType/responseSchema/responseJsonSchema`；v1 归一化这些已知形式，多个形式不一致返回 400，旧 `_responseJsonSchema` 只作为明确的兼容别名。REST 新字段通过并不意味着每个旧 SDK 都支持输入这些字段。[A4][G3]

### 5.4 鉴权与模型目录

- OpenAI：`Authorization: Bearer fk_...`。
- Anthropic：接受 `x-api-key` 和 `Authorization: Bearer`；同时出现但值不一致返回 400。要求支持的 `anthropic-version: 2023-06-01`；不存在/未知版本给出明确 400。查询参数 `beta=true` 不影响路由，但不表示所有 beta 功能可用；未知功能性 beta 请求拒绝。[A1]
- Gemini：接受 `x-goog-api-key` 和 `?key=`；同时出现且不同返回 400。查询 Key 不能进入任何访问日志。SDK 示例优先 header。
- v1 默认始终启用鉴权；不附带固定公开默认 Key。首次 `keys create` 生成真实随机 Key。用于离线 mock 上游的 `offline-test-key` 只是测试上游凭据，不能用作 FakeAPI 默认入口 Key。

展示 ID 使用 `[A-Za-z0-9][A-Za-z0-9._-]{0,127}`，不含 `/` 和 `:`。这牺牲了少量自由度，但能在四种协议中安全使用同一 ID。真实模型 ID 可以包含供应商要求的斜杠，独立保存在 target 配置中。

模型目录不请求真实供应商，不暴露真实模型/上游 URL。所有成功生成响应回显展示 ID，而非真实 target model。

| 目录 | 返回形状和分页 |
|---|---|
| OpenAI | `object:list,data:[{id,object:model,created,owned_by:"fakeapi"}]`；本地模型数量有限，完整返回；详情不存在返回 404 |
| Anthropic | `data,first_id,last_id,has_more`；支持 limit/after_id/before_id，before/after 互斥；按锁定 ModelInfo Schema 补齐全部 required 字段，包括 id/type/display_name/created_at/capabilities/lifecycle/line/各 token 上限及弃用日期；仅 nullable 字段使用 null，不能将新 required 字段当可省略字段 |
| Gemini | `models:[{name:"models/<id>",baseModelId,displayName,version,inputTokenLimit,outputTokenLimit,supportedGenerationMethods}]`，稳定分页 nextPageToken；不声明未实现的 countTokens |

日期使用固定的配置快照/发布元数据，不在每次 list 时用当前时间改变排序。分页 token 携带目录摘要和偏移，目录变化后旧 token 返回 400，避免漏项/重复。模型能力元数据只描述模拟契约，不能把“接收图片”标成“能够理解图片”。[A2][G5]

### 5.5 非流式响应最低契约

- **Chat**：id/object/created/model/choices；choice.index=0，message.role=assistant，content 或 tool_calls，finish_reason；usage。
- **Responses**：id/object/created_at/status/model/output/error/incomplete_details/usage；输出文本位于 assistant message item 的 `content:[{type:output_text,text,annotations:[]}]`。`output_text` 是 SDK 可生成的便捷访问能力，不能靠只造顶层 output_text 替代标准 output 数组。
- **Anthropic**：id/type:message/role:assistant/model/content/stop_reason/stop_sequence/usage。
- **Gemini**：candidates[0].index、content.role:model、parts、finishReason、usageMetadata、modelVersion、responseId。

P0 依据锁定 SDK 的正式类型将上述最低集合补齐为 fixture Schema，尤其区分省略、null 和空数组；不复制文档示例中与实际 Schema 不符的占位数值。

### 5.6 流式事件顺序

**Chat 文本成功：**

```text
data: chunk(delta.role="assistant", delta.content="", finish_reason=null)
data: chunk(delta.content=文本分片, finish_reason=null) × N
data: chunk(delta={}, finish_reason="stop"|"length"|"tool_calls")
data: chunk(choices=[], usage=最终统计)  # 仅 include_usage=true
data: [DONE]
```

`include_usage=true` 时其它 chunk 的 usage 按协议设为 null；不能把总 usage 在每片重复相加。工具 name/id 在开始声明，arguments 为可累积 JSON 字符串。Chat 的 reasoning_content 仅为明确开启的扩展，不描述成 OpenAI 官方标准字段。[O1][O2]

**Responses 文本成功：**

```text
response.created
response.in_progress
response.output_item.added          # assistant message, in_progress
response.content_part.added         # output_text
response.output_text.delta × N
response.output_text.done
response.content_part.done
response.output_item.done
response.completed                 # 包含完整、可累积的最终 Response
```

event 名与 data.type 一致；sequence_number 从 0 开始单调递增；item_id/output_index/content_index 前后一致。函数调用使用 output_item 与 function_call_arguments.delta/done；reasoning summary 使用独立 reasoning item、reasoning_summary_part.added/done、reasoning_summary_text.delta/done。成功只发一次 completed，达到输出上限使用 `response.incomplete` 及 `incomplete_details.reason:max_output_tokens`；失败使用 `response.failed`。**不附加 Chat 的 [DONE]。**[O3]

**Anthropic 文本成功：**

```text
message_start
content_block_start
content_block_delta × N
content_block_stop
message_delta                      # stop_reason + 累计 usage
message_stop
```

多块按 index 开闭；message_start 的 content 为空；工具参数使用 input_json_delta.partial_json；长暂停可以插入 ping。没有 `[DONE]`。不发送缺失签名却声称完整原生 thinking 的块。[A5]

**Gemini 文本成功：**

```text
data: GenerateContentResponse(当前 text 增量) × N
data: GenerateContentResponse(finishReason + 最终 usageMetadata)
EOF
```

每段 text 是增量，不是已输出全文；允许最后一段同时携带最后文字与 finishReason。单工具 fixture 一次发送完整 functionCall.args，不拆成不合法对象。没有 `[DONE]` 或 message_stop；v1 仅实现 `alt=sse`，其它 streaming 传输格式返回明确 400。[G1][G6]

### 5.7 usage 与结束原因

**请求额度与 token usage 完全分开。** usage 是协议模拟/展示字段，永远不用于次数扣减。

预设估算器固定版本 `estimate_v1`：文本 UTF-8 字节数除以 4 向上取整；完整输入文本、工具声明和参数的紧凑 JSON 纳入估算，每条消息增加 4 个模拟 token，每张图片增加固定 256 个模拟 token。不要计算 base64 长度来假装视觉 token 数。空输入允许为 0。此算法用于重复测试，不代表任何供应商 tokenizer。

输出的普通文字与原生/扩展思考分别估算。portable 思考属于可见普通文本，不额外记一次 reasoning。工具 name/arguments 纳入输出；SSE 包装、ID、事件名、代码围栏以外的协议 JSON 不算正文 token。ASCII 自动代码围栏若实际发送给用户，则算正文。

| 字段 | 映射 |
|---|---|
| Chat prompt/completion/total | input；visible+reasoning；input+visible+reasoning；reasoning_tokens 是 completion 子集 |
| Responses input/output/total | 同上；reasoning_tokens 是 output 子集 |
| Anthropic input/output | input；visible+reasoning |
| Gemini prompt/candidates/thoughts/total | input；visible；reasoning；input+visible+reasoning |

缓存 token 是 input 子集，不能再加进总数。实际 AI 非流式收到可信 usage 时进行映射；同协议 Chat 流式透传最终 usage。Responses/Gemini 跨协议流可在尾部映射最终统计。**Anthropic 从 Chat 转换的流式请求，在 message_start 时无法提前得知 Chat 的最终输入 usage，v1 统一使用估算 input/output 保持自洽**，并返回 `X-FakeAPI-Usage: estimated`；不先编一个输入数再在客户端无法识别的位置更正。该分支仅列“结构兼容、统计估算”。

Chat 上游令 C=completion_tokens、R=completion_tokens_details.reasoning_tokens。R 存在时验证 0≤R≤C，内部 visible=C−R；非法/负数 usage 整体改用估算，不能输出负数。C 有效但缺 R 时，无独立推理输出则 R=0；有推理输出则以 min(C, estimate_v1(推理文本)) 估计 R，visible=C−R，source=mixed，保留原 C 总数。把推理改为 portable 文本展示时，只在内部重新分配 visible/reasoning，不再给 C 叠加一次 token。测试明确覆盖所有分支。

上游未提供 usage 时使用同一估算器。预设始终返回 `X-FakeAPI-Usage: estimated`；其它 AI 流在响应头不能事先确定统计来源时不伪造精确来源头，文档声明最终统计可能为估算。客户端中断时可能收不到最终 usage，不因此补发成功事件。[O1][A5]

| 内部结束原因 | Chat | Responses | Anthropic | Gemini |
|---|---|---|---|---|
| 正常完成 | stop | completed | end_turn | STOP |
| 命中停止串 | stop | 不提供自定义 stop 参数 | stop_sequence，填命中序列 | STOP |
| 输出 token 上限 | length | incomplete/max_output_tokens | max_tokens | MAX_TOKENS |
| 单工具调用 | tool_calls | completed + function_call item | tool_use | STOP + functionCall part |
| 上游过滤 | content_filter（仅上游确有此信号） | incomplete/content_filter，验证当前 Schema | 拒绝信息无法等价则规范错误，不伪造原因 | SAFETY（仅可确认的映射） |

预设先计算最终文本、停止串与上限，再分块，保证 stop 跨 chunk 也生效。按 grapheme 边界截断，不破坏 UTF-8。JSON/工具 fixture 的预算不足或 stop 会破坏完整对象时，在提交前报 400 `fixture_constraint_conflict`；不输出半个对象并宣称满足 Schema。响应时限超时属于错误，不冒充 token length。

Chat 上游的 finish_reason=stop 无法单独证明命中了哪条停止串。转 Anthropic 时默认 end_turn、stop_sequence=null；只有本地预设实际匹配到序列，或上游确实提供了可验证的命中信息，才使用 stop_sequence。不能因为请求里带了 stop 就猜测它已命中。

### 5.8 思考的表达与显示边界

| 模式 | 行为 | 限制 |
|---|---|---|
| `portable_text`，默认 | 正文前输出 `[模拟思考]` + 预设文案 + 空行 | 普通客户端可见；通常不会进入折叠思考面板，会影响完整正文字符串 |
| `native` + Chat | 通过 message/delta.reasoning_content 输出 | 非官方扩展；SDK 可能保留额外字段，但普通 content helper 会忽略 |
| `native` + Responses | reasoning summary item 及对应事件，文案标注模拟 | 普通 output_text helper 可能不展示 summary；不生成 encrypted_content |
| `native` + Anthropic | 回退为明确的 portable_text；配置检查给出提示 | 不生成伪造 signature；不能承诺原生 thinking 面板 |
| `native` + Gemini | thought:true 的 text part，与正文独立 | 不生成 thoughtSignature；SDK text helper可能排除 thought，需要读取 parts |
| `off` | 没有任何预设思考文字或专用字段 | 仍按所选模板执行明确保留的首延迟 |

客户端显式关闭显示（如支持的 Gemini includeThoughts:false、Anthropic thinking disabled/display omitted）优先抑制模拟思考。原生开启请求不会让 FakeAPI 真正推理；预算/effort 只在文档列出的模拟范围内处理，不保证与供应商模型计算量一致。

Anthropic thinking 签名用于块完整性，不能由本地随意生成一个字符串就声称可转发给真实 Anthropic 验证。Gemini thoughtSignature 同样是供应商不透明数据；FakeAPI 可以接收和暂存在内存，但不将其变成跨供应商的有效签名。[A5][G2][G7]

真实 AI 分支默认保留上游内容与节奏：同协议 Chat 的已知 reasoning_content 扩展可原样传递；只有源字段明确是 summary 时才映射成 Responses summary。**不把任意推理文本重新命名为“摘要”来假装无损。** 跨协议缺少等价字段时默认省略非正文推理输出，并用 `X-FakeAPI-Reasoning: omitted-in-translation` 告知；不会把上游正文丢掉。可配置 `upstreams.<id>.reasoning_translation: portable_text`，将上游提供的推理用 `[上游推理]` 标签作为普通文本展示，但 JSON/工具/精确输出约束下禁止该选项。需要原始推理字段的场景优先使用同协议 Chat。未来增加原生上游后才支持签名内容无损回放。

该 reasoning header 表示在提交前已经确定的转换策略，或提交前已经观察到的省略行为；若直到正文提交后才发现上游额外推理字段，则允许省略该 header，不能修改已经发出的响应头。兼容文档必须独立说明这一限制。

### 5.9 错误形状和状态码

```json
{"error":{"message":"Request quota exceeded","type":"rate_limit_error","param":null,"code":"request_quota_exceeded"}}
```

```json
{"type":"error","error":{"type":"rate_limit_error","message":"Request quota exceeded"},"request_id":"req_fake_..."}
```

```json
{"error":{"code":429,"message":"Request quota exceeded","status":"RESOURCE_EXHAUSTED"}}
```

以上依次为 OpenAI、Anthropic、Gemini 封套。request ID 只用于响应/内存，不增加到请求日志。

| 情况 | HTTP | OpenAI type/code 方向 | Anthropic type | Gemini status |
|---|---:|---|---|---|
| JSON/参数/输出约束错误 | 400 | invalid_request_error / 具体错误码 | invalid_request_error | INVALID_ARGUMENT |
| Key 无效、过期、撤销 | 401 | authentication_error / invalid_api_key 等 | authentication_error | UNAUTHENTICATED |
| 已验证 Key 无权限 | 403 | permission_error | permission_error | PERMISSION_DENIED |
| 模型或资源不存在 | 404 | invalid_request_error / model_not_found | not_found_error | NOT_FOUND |
| 请求体超限 | 413 | invalid_request_error / request_too_large | request_too_large | RESOURCE_EXHAUSTED |
| 总/AI 次数不足、RPM | 429 | rate_limit_error / 分别的 quota/rate 码 | rate_limit_error | RESOURCE_EXHAUSTED |
| 本地内部异常 | 500 | server_error / internal_error | api_error | INTERNAL |
| 上游认证/坏响应/协议错误 | 502 | server_error / upstream_* | api_error | UNAVAILABLE |
| 并发位不足、上游暂时限速/不可用、DB busy | 503 | server_error / 具体错误码 | api_error | UNAVAILABLE |
| 上游首事件/空闲/总时限超时 | 504 | server_error / upstream_timeout | timeout_error | DEADLINE_EXCEEDED |

服务状态码映射是 FakeAPI 的明确契约，不宣称每个状态与真实供应商完全相同。上游 401 不转成入口 401，避免把部署者的上游密钥错误误报成用户 Key 错误。临时 RPM/并发/上游限速可返回整数秒 Retry-After；永久额度不足不提供虚假的短重试时间。原始上游错误正文、URL、headers 均不回传；只返回安全摘要。[A6][G8]

## 6. 配置、素材、命令行和模型路由

### 6.1 配置规范

统一 YAML，顶层 `schema_version:1`。附录 A/B 给出完整代表性配置，P1 将同一份配置结构落实为 Go 类型与 `schemas/config.v1.schema.json`。

| 顶层字段 | 含义 |
|---|---|
| server | listen、输入/输出字节上限、服务并发、读/写/退出时限 |
| storage | SQLite 路径和 busy_timeout |
| request_log | 开关和输出位置；不提供字段扩展开关 |
| defaults/profiles | 默认节奏、随机策略、三个模板 |
| assets | manifest 路径；资源 ID 从 manifest 解析 |
| key_policies | 创建 Key 时使用的管理模板 |
| models | 展示 ID、显示名、模板、可选 target |
| upstreams | 供应商地址、协议、密钥环境变量、并发与超时 |
| targets/default_target | 真实模型和能力、默认目标 |
| matchers | 可查看/编辑的文本规则原子组合 |
| rules | 动作、优先级、启停、元数据条件、覆盖参数和 AI 失败策略 |
| fallback | 唯一全局预设兜底 |

规范：

- 拒绝未知字段、重复键/ID、YAML anchor/alias/merge、非标准 tag、多文档和类型错误；布尔值只接受 true/false。
- 时间间隔用 Go 风格字符串 `300ms/5s/2m`；到期时间 RFC3339，禁止没有时区的字符串。大小使用整数 bytes，次数使用非负整数或 null。
- 相对路径以**配置文件目录**为基准，非当前工作目录；示例中的 `../../assets/manifest.yaml` 因此可预测。manifest 内 file 相对 manifest 目录。
- 环境变量只允许显式 `{env: NAME}` 的字符串引用，以及 `api_key_env: NAME`，不在整个 YAML 中替换 `${...}`；不执行 shell，不展开任意模板。
- `base_url` 允许 http/https，不允许 userinfo、query 或 fragment；应包含上游 API 基路径，例如 `/v1`。客户端传入的 URL/模型不决定上游目的地。
- 加载时编译正则、校验资源存在、Schema 和工具参数、所有引用和启用 AI 规则的有效 target；有效配置快照一经启动保持不变。v1 修改配置后重启，不做文件监视/热更新。
- disabled AI 规则在纯预设模式允许没有任何可解析目标，但其语法、已写出的显式引用仍须正确；附录 A 的禁用动作不填写不存在的 target。只要启用了一个无法解析 target 的 AI 规则，启动失败，不悄悄变成纯预设。
- `config check` 永不访问网络；会检查已启用分支的必需环境变量。`--skip-env` 仅跳过环境变量值存在性校验，不跳过结构和引用校验。
- `config show --redact` 只输出引用和脱敏结果，不输出上游密钥值。

配置错误示例：`E_CONFIG_REF rules[3].action.target: unknown target 'primary-fast'`；显示文件路径/行号/字段名，不回显敏感值。退出码：0 成功、1 运行失败、2 配置/用法错误。

附录采用以下具体字段，作为 Schema 的默认值来源：

| 字段 | 默认值及约束 |
|---|---|
| server.max_request_bytes / max_response_bytes | 8 MiB 入站 JSON；1 MiB 累计语义输出（正文、思考、参数），不按 chunk 数绕过 |
| server.max_sse_event_bytes | 1 MiB 上游单事件及预提交缓冲；下游编码 JSON 转义可能增加字节数，仍受语义输出总上限约束 |
| server.max_concurrent / max_messages / max_tools | 64 / 256 / 32；每个值均为正整数 |
| server.read_header_timeout / read_body_timeout / write_timeout / shutdown_timeout | 5s / 15s / 10s（每次写）/ 10s；HTTP header 上限固定 16 KiB |
| server.anthropic_messages_alias | true 时额外提供 POST /v1/messages；不改变模型目录格式 |
| profiles.*.thinking | enabled；enabled=true 时要求 channel 为 portable_text/native 且 text_asset/text 二选一；enabled=false 即 off，可省略 channel 和文字 |
| profiles.*.timing | 第 7.1 节全部字段；概率 0–1、chunk_size 正整数、速度非负、间隔非负、max_total 正数 |
| models[].limits | input_tokens:32768、output_tokens:8192；模拟输入按 estimate_v1 检查，声明超过输出上限的请求准入前 400；真实模型仍可能有不同 tokenizer/更小上限 |
| models[].created_at | 固定 RFC3339 时间；省略时用构建发布元数据，不用进程启动时间 |
| targets.*.max_tokens_parameter | max_tokens 或 max_completion_tokens，默认前者；禁止盲目同时发送两者 |
| targets.*.capabilities | images、tools、json_object、json_schema、developer_role、parallel_tool_calls_parameter；未声明默认 false；json_schema=true 要求 json_object=true |
| targets.*.developer_role_fallback | reject，或显式 system；仅不支持 developer_role 时生效 |
| upstreams.*.timeouts | connect:5s、first_event:20s、idle:30s、total:120s；子时限不得大于总时限 |

上游能力由部署者按实际目标声明；配置检查只验证声明自洽，不偷偷发送收费探测。`max_tokens` 的有效值不超过展示模型限制；字段名由 target 选择。JSON 深度固定上限 32，工具 Schema 引用深度上限 16，图片仍受整体请求体字节上限约束。

`behavior.thinking` 和 `behavior.timing` 均为部分覆盖：先合并有效 profile，再检查完整值。覆盖中只写 `{enabled:false}` 是合法的。启用思考却没有可用文字、同时设置 text/text_asset、引用不存在素材，才属于错误。

### 6.2 动作规范

每个 action 只能选择一种 type：

| type | 必需参数 | 语义 |
|---|---|---|
| preset | `text`、`asset` 或 `choices` 三选一 | choices 中每项 text/asset 二选一，可有正整数 weight |
| arithmetic | 无额外表达式 | 仅处理已声明的有限整数四则 grammar；不执行通用表达式 |
| json_fixture | json 对象/数组与可选 schema | 先验证配置，再验证请求强制 schema；只输出 JSON 文本 |
| tool_fixture | name/arguments/final_text | 一个标准函数调用，一次结果回传，一次固定收尾 |
| upstream | 可选 target；可选 on_error | 完整上下文交给指定或解析出的真实模型 |

允许预设 text 中使用 `{{model}}` 与 `{{rule_id}}` 两个固定占位符，禁止表达式、函数、循环和请求正文模板。ASCII/SVG/JSON 资源默认不做插值，避免破坏正文。

每条规则可用 `behavior.profile` 选择模板，或用 `behavior.thinking` / `behavior.timing` 覆盖已定义字段。解析顺序是 defaults.profile → model.profile → rule.behavior.profile → rule 的具体字段覆盖。`json_fixture/tool_fixture/arithmetic`、kind=svg 的素材及 `action.strict_text:true` 的预设自动关闭所有模拟思考；在这些规则上显式设置 thinking.enabled:true 为配置错误。`strict_text` 是 preset 动作的可选布尔字段，不是素材 kind。

preset 的 `render:auto|plain|markdown_code` 默认为 auto：ASCII 包围栏，其它类型原样输出。`on_error` 只允许 upstream 动作设置：`{mode:api_error}`、`{mode:frog}`，或 `{mode:preset,response:{type:preset,text:"固定备用文字"}}`；response 只能是同一 preset 结构，禁止再次包含上游/失败策略。

### 6.3 多模型、多上游和 AI 分流

目标由三步解析：**rule.action.target → models[].target → default_target**。选择到某一值后不存在则报错，不能悄悄回退另一个供应商。

`upstreams` 定义服务连接；`targets` 定义该连接上的真实模型及能力。同一供应商可以有 fast/reasoning 两个 target，另一个供应商可以独立建连接。一个模型只有展示 ID，真实 model 来自 target。Key 分流通过可信标签/组条件的高优先级规则实现，不增加第二套隐藏路由器。

混合模板默认准备 `ai-greeting`、`ai-identity`、`ai-tests`、`ai-pelican` 四条规则，各自 `enabled:true`，默认只对带 `ai` 标签的 Key 生效。纯预设模板包含同名规则但 disabled；配置了一个上游不会自动让任意请求调用它。关闭某条 AI 规则后，下方相同 matcher 的本地预设自然生效。

对“用 SVG 画骑自行车的鹈鹕”的 AI 分支，保留这条原用户请求及前文，不替换成内部简化 prompt、不加入“请画奶蛙”等指令。身份询问交给上游时也由上游正常回答，不强制冒充展示 ID 对应的真实模型。

第一版实现 `upstreams.*.kind:openai_chat`；原生 Responses/Anthropic/Gemini 上游适配后续再做。此时能在控制范围内完成四种入站格式到一种上游格式的转换，避免维护四乘四组合。

### 6.4 跨协议转 OpenAI Chat 的保留与限制

| 信息 | 处理方式 |
|---|---|
| 完整普通文本上下文 | 保留所有已提供消息及顺序；规则仍只看最新语义 user |
| Responses instructions | 映射至目标支持的 developer/system；包含已有 system 时保留顺序，不合并丢失边界 |
| Anthropic system / Gemini systemInstruction | 转为前置 system；禁止把它们混进最新用户匹配 |
| developer | 目标 capability 支持时保留；不支持时默认拒绝，需显式配置才允许转 system |
| 图片 | 仅目标 images=true；inline/base64→data URL，HTTP(S) URL→image_url；不本地下载，不忽略图片 |
| Google Files/供应商 file_id | 无通用映射，AI 预检 400；普通预设可仅解析元数据 |
| 函数声明/工具历史 | 映射名称、描述、参数 schema、call ID、arguments/result；名称/ID/顺序不一致时报错 |
| 多模态工具结果 | v1 跨协议默认拒绝；仅文本/JSON 工具结果可映射 |
| max tokens/temperature/top_p/stop | 按字段策略转换；缺省不注入不兼容参数 |
| JSON | 根据 target 能力转换到 response_format；不靠 prompt 伪装支持 strict schema |
| topK、safetySettings、缓存控制、citations、context management、原生服务器工具 | 缺等价语义则拒绝；只有专门白名单的提示性字段可忽略 |
| signed/encrypted reasoning history | 不可无损转到通用 Chat；AI 分支默认拒绝，不静默删掉工具续轮所需信息 |

工具历史对齐基于完整请求中的标准关联字段。Gemini 没有 ID 的单函数调用，可依据当前唯一 name/位置确定性补一个内部 call ID；有歧义返回 400。第一版以每轮最多一个函数调用为转换保证，发送受支持的 `parallel_tool_calls:false`；不支持此参数的 target 不发送，但如果实际上收到多个调用，按超范围上游响应处理。普通 function 声明可以有多个，fixture 必须选择其中一个合法工具。

同协议 Chat 也必须验证并重写 model/鉴权/目标，不能直接成为任意 URL 代理。上游收到的是完整、已支持上下文与必要参数；客户端原 Key、Key 标签、FakeAPI seed header 永不转发。

上游 HTTP 客户端验证 TLS 证书，不自动跟随 3xx 到另一个地址；重定向作为规范上游错误交给本规则失败策略。错误诊断只能显示配置中的 upstream/target ID 和固定错误码，不能输出可能含凭据的底层 URL 错误字符串。

### 6.5 素材交付规范

**P1 的素材工作由 Codex 完成初稿，项目维护者完成发布视觉复核；它是阶段交付物，不是发布后待补项。**

默认数量控制为：6 张原创奶蛙 ASCII（发呆、问好、思考、喝奶、摊手、庆祝）、2 份模拟思考文案、4 条短彩蛋、3 个测试资源（原创鹈鹕骑车 SVG、JSON、工具参数各一个）。问候与身份预设可直接写在 YAML。

默认 ASCII 只使用 ASCII 可打印字符与 LF，不含 emoji、tab、ANSI escape、双向控制字符或难以对齐的全角符号；建议宽 24–40 列、高 6–12 行。第一版用通用原创青蛙轮廓和奶杯元素表现主题，不声称取得任何现有表情包 IP 的许可。

manifest 每项至少有 `id,file,kind,author,origin,license`；可增加 source_url、created_at、width_policy 和 sha256。原创填写 origin=original、license=MIT；外部素材必须明确原链接/作者/分发许可。导入素材不会自动变成 MIT；外部内容只有许可适合随项目发布，才能进入默认包。

```yaml
schema_version: 1
assets:
  - id: frog-idle
    file: milkfrog/idle.txt
    kind: ascii
    author: FakeAPI contributors
    origin: original
    license: MIT
    width_policy: ascii
  - id: pelican-svg
    file: fixtures/pelican-bicycle.svg.txt
    kind: svg
    author: FakeAPI contributors
    origin: original
    license: MIT
    width_policy: unicode
```

`kind` 支持 text/markdown/ascii/svg/json。UTF-8 无 BOM，入库规范化 LF；保存与校验时保留空格，不 rstrip 整个素材。用户导入 Unicode 文本允许 grapheme，但标注终端/字体宽度差异；强制排版素材优先 ASCII。

`render:markdown_code` 可为 ASCII 自动包裹 text 围栏，确保支持 Markdown 的客户端使用等宽字体；`render:plain` 输出原文。包裹时选择不会与内容冲突的围栏长度。SVG 和 JSON fixture 默认 plain，不增加 Markdown/思考前缀。

导入例子：`fakeapi assets import --id frog-custom --kind ascii --author "Your Name" --license MIT --config ./fakeapi.yaml ./my-frog.txt`。标准 flag 解析要求选项在位置参数之前，全部 CLI 示例遵循这一顺序。导入只能复制普通 UTF-8 文件到 manifest 所在目录下的 `imports/`，用临时文件后原子替换 manifest；拒绝目标逃逸路径、危险控制字符与同 ID 覆盖，除非显式覆盖。服务已启动时导入后需重启加载快照。

### 6.6 命令行

| 命令 | 行为 |
|---|---|
| `fakeapi init --template preset\|hybrid --dir PATH` | 创建配置、素材、环境变量示例；不覆盖已有文件，不自动生成长期公开 Key |
| `fakeapi serve --config FILE [--listen ADDR] [--state FILE]` | 加载配置、取单实例锁、迁移检查、启动 |
| `fakeapi config check --config FILE [--skip-env]` | 离线结构/引用/资源/环境检查；不消费次数 |
| `fakeapi config show --config FILE --redact` | 输出已解析的安全配置视图 |
| `fakeapi keys create --config FILE --policy NAME --label NAME [--expires-in 30d]` | 生成随机 Key，原文仅显示一次；policy 快照落库 |
| `fakeapi keys list/show/revoke/update` | 查看元数据/额度、撤销或调整限制；查看不能恢复原文 |
| `fakeapi keys import --from-env NAME` | 导入符合格式和长度要求的已有 Key，供部署恢复；不把 Key 作为普通命令行参数 |
| `fakeapi keys reset-usage --id ID --confirm` | 显式管理操作，原子清零选定 Key 计数；不作为自动账期功能 |
| `fakeapi rules explain --input FILE --protocol NAME --model ID [--tags ai] [--group NAME] [--seed 42]` | 使用人工输入和手工元数据解释最新文本选择/命中规则/计划动作；不调用上游，不消费次数；tags 是逗号分隔列表 |
| `fakeapi assets list/lint/import` | 素材目录、格式与来源检查 |
| `fakeapi db backup --out FILE` | SQLite 在线备份 API 或等价一致性备份；不直接复制活动中的单个 .sqlite |
| `fakeapi db migrate --config FILE` | 备份后执行明确版本迁移；检测更新版本库则拒绝降级打开 |
| `fakeapi health --url URL` / `fakeapi version` | 健康检测 / 输出服务版本、commit、构建工具链 |

CLI `--config` 和 `--state` 是通用参数。服务管理命令不需要网络管理 Key，因为权限来自对本机配置/数据库的文件访问权。`--expires-in` 支持 s/m/h/d，按 UTC duration 计算；配置中的绝对日期仍要求 RFC3339。

Key 管理命令支持 `--format text|json`，create 的 JSON 输出也只交付一次原文；其它查询只输出公开 ID 和元数据。create/update 可用互斥的 `--expires-at RFC3339` / `--expires-in DURATION` 覆盖 policy.expires_at；update 还可显式调整 tags/group/total-limit/ai-limit/rpm/max-concurrent，null/unlimited 通过明确参数值表示，不能用 0 表示无限。revoke/show/update/reset-usage 均以公开 `--id` 选择对象。

环境变量固定白名单：`FAKEAPI_CONFIG`、`FAKEAPI_LISTEN`、`FAKEAPI_STATE`、`FAKEAPI_UPSTREAM_BASE_URL`、`FAKEAPI_UPSTREAM_API_KEY`、`FAKEAPI_UPSTREAM_MODEL`；另允许在配置 env 引用中指定自定义变量名。优先级为显式 CLI 参数 > 对应 FAKEAPI 环境变量 > YAML > 内置默认值。通用环境覆盖只覆盖明确字段，不实施任意双下划线配置展开。

其中 CONFIG/LISTEN/STATE 是固定的全局覆盖；三个 UPSTREAM 变量只是示例约定，必须在某个 upstream/target 里显式引用，不会把所有供应商配置一并覆盖。`FAKEAPI_UPSTREAM_SVG_MODEL` 是附录混合配置的自定义 env 引用示例。

## 7. 节奏、流式失败、工具状态与请求计数

### 7.1 三种默认模板

模板参数是实施默认值，单位和名字与附录一致。`first_output_delay` 指到第一条**有语义输出**的延迟，不把提前发送空事件/心跳算作实现自然等待。

| 参数 | instant 极速 | natural 自然，默认 | playful 整活 |
|---|---:|---:|---:|
| first_output_delay | 0ms | 300ms | 1500ms |
| thinking.enabled | false | true | true |
| thinking.text_asset | 无 | thinking-natural | thinking-playful |
| thinking_to_body_delay | 0ms | 300ms | 1500ms |
| graphemes_per_second | 0，表示不人为限速 | 60 | 24 |
| chunk_size | 16 个 grapheme | 4 个 grapheme | 4 个 grapheme |
| jitter | 0ms | ±20ms | ±120ms |
| 每片后 pause_probability | 0 | 0.02 | 0.10 |
| 命中时 pause_duration | 0ms | 100ms | 800ms |
| max_total | 5s | 30s | 45s |

`thinking_to_body_delay` 仅在实际输出了思考时使用。定时器间隔根据 chunk grapheme 数 / 速度计算，加入对称 jitter 后最小为 0，再加可选 pause；最后一片后不多等待一次。思考使用同一分块/速度配置，规则可覆盖。非流式请求经历同样的有上限调度，然后一次性返回；instant 用于不需要等待的测试。

预设最长时限从准入后开始；包含首次等待、思考、正文、停顿和写出。超时立即取消，不把数分钟内容无限排队。服务另有入站读取时限、每次网络写入时限；客户端断开时立即停止所有等待。AI 分支使用上游 timeouts.total，不套用 natural/playful；不先输出模拟思考再请求真实 AI。

### 7.2 随机与可复现性

默认 `defaults.random.mode:random` 使用每请求独立随机种子。测试配置为 `deterministic` 并指定整数 seed；可选 `X-FakeAPI-Seed` 请求头只在 `allow_request_seed:true` 时覆盖。使用该扩展时不要求各协议 SDK 都有同名参数，Python extra_headers/TS headers/REST 即可。

有效随机上下文由 seed、规范化当前用户文本、展示模型和命中 rule ID 派生；不包含时间、请求到达顺序或进程级随机计数器。正文选择和节奏分别使用独立子序列。因此相同输入/模型/规则/seed，在并发、取消和重启后仍有相同素材选择及计划停顿，修改节奏不会改变所选素材。

实现采用每请求 `math/rand/v2` PCG 实例，算法标识冻结为 `pcg_v1`，种子派生用 SHA-256；将前若干随机输出、weighted choice 和延迟计划放入 golden test，升级 Go 时检查。HTTP request ID/created 时间默认保持真实唯一性，不属于 seed 可复现承诺；测试需要字节级快照时注入 fake clock/ID generator。不得把 seed 或文本摘要新增到生产请求日志。

FakeAPI 种子只控制本地预设和节奏；不承诺真实模型输出可复现，也不将该扩展 seed 自动转发给上游。

### 7.3 流式状态及提交边界

状态固定为：`RECEIVED → VALIDATED → PREPARED → ADMITTED → WAITING_FIRST_EVENT → STREAMING → SUCCEEDED|FAILED|CANCELLED`。非流式在 ADMITTED 后执行完整结果，最后提交 JSON。任意失败只能进入一个终态，不能随后再发成功终态。

`committed` 的定义是 HTTP 状态/响应头或任意响应体已经写出；**role-only、message_start、response.created 和心跳也算提交**。因此在确定可输出前，不能先向客户端 flush 一个起始事件，再期待上游失败后改 HTTP 状态。

AI 流式先检查上游 HTTP 状态，并等待第一个有效语义事件：非空正文/推理、合法的工具起始数据，或允许零输出的正常终止。零输出须验证完整的上游终止序列，包括 Chat 的 finish 和 [DONE]。role-only、空 delta、usage-only、comment/ping 均不触发下游提交。等待最多 first_event timeout，预提交缓冲上限等于 max_sse_event_bytes，默认 1 MiB；空事件只保留必要元数据，不无限累积。超限视为坏上游响应，仍可执行满足条件的备用回复。拿到首语义事件后才向客户端写协议起始事件；之后按接收节奏转换，不能为了获得完整 usage 而缓存整篇正文。

网络实现要求：

- `Content-Type:text/event-stream; charset=utf-8`、`Cache-Control:no-cache, no-transform`、`X-Accel-Buffering:no`；不设置 Content-Length，不为 SSE 开启压缩/代理缓冲。
- JSON 通过序列化器转义后放入 data，不直接拼接含换行的用户/素材文字。
- 入站上游 SSE 解析支持 CRLF、多行 data、注释、TCP 任意分片、超过 bufio.Scanner 默认长度的合法事件；单事件上限 1 MiB、总响应上限按配置。
- 使用请求 context 驱动 timer、上游 HTTP request、流迭代与有界队列；队列最多 8 个待编码事件或等价有界字节缓冲，禁止无限生产。
- 每次 Write/Flush 检查错误；慢客户端用 `http.ResponseController` 设置单次写 deadline（默认 10s），SSE 不依赖一个短的全局 WriteTimeout。
- 提交后长间隔 15s 可发心跳：Anthropic 使用 ping，Chat/Responses 使用经 SDK 验证的 SSE comment。Gemini 禁用应用层心跳，所有事件固定为 `data: `（冒号后一个空格）+ 单行 JSON + 空行，不发送 comment/event/id/retry 行。当前 Google Python SDK 会把非 data 行当 JSON 解析，标准 SSE 注释也可能触发异常；SDK 测试须覆盖长停顿。预提交阶段不为了心跳提前提交。[G9]
- 客户端取消/写失败后取消 upstream context、关闭 body、停止 timer、释放三个层级的并发位。进程退出先停止新准入，最多等 shutdown_timeout，再取消剩余请求。
- Responses 的最终对象确需短期累积 output；设置 max_response_bytes 防止无限内存增长。其它编码器也不得重复保留无必要的大 body 副本。

### 7.4 失败分类与备用回复

`on_error.mode` 只有 `api_error|preset|frog`，默认 api_error。preset 必须引用一个有限的本地 preset 动作，不允许再次调用上游或递归 fallback。备用回复采用 instant，不附加模拟思考；仍受原请求剩余总时限和输出约束限制。

| 失败时机/原因 | 行为 | 使用规则备用回复？ |
|---|---|---|
| 未配置任何上游，且 AI 规则均关闭 | 正常纯预设运行 | 无需备用 |
| 启用 AI 规则但缺 target、环境变量、非法 URL 等 | config check/启动失败，退出码 2 | 否，配置问题不能隐藏 |
| 请求带目标不支持的图片/Schema/签名上下文等 | 准入前 400，说明能力边界 | 否 |
| 上游并发位不足/Key 额度不足 | 准入前 503/429 | 否，不用备用绕过额度策略 |
| 已准入，连接失败/401/429/5xx，或 connect/first_event/idle 子超时，客户端尚未提交且 total 仍有剩余 | 默认规范 502/503/504；按规则可返回备用 preset/frog | 是，但备用必须满足本次强制约束 |
| total 总时限已耗尽 | 未提交则返回 504；已提交则发送允许的失败事件并关闭 | 否，不另开备用回复预算 |
| 已收到部分上游片段，但客户端尚未提交 | 仍属预提交失败，只要缓存上限/时间条件满足，可丢弃缓存返回备用 | 是 |
| 已向客户端发起始事件/任何正文后失败 | 发协议可表达的失败事件并关闭；不能改 HTTP 200 | 否，不能在半截回答后接奶蛙伪装成功 |
| 客户端主动取消 | 终止资源，不继续写错误/奶蛙 | 否 |

中途失败编码：Chat 尽力发送 `data:{error:...}` 后关闭，不补 finish_reason:stop 或 `[DONE]`；Responses 发 response.failed；Anthropic 发 event:error 后关闭，不补 message_stop；Gemini 尽力发送 error envelope 的 data 后关闭，不补 STOP。若连接已断开，直接清理。

Responses 的 response.failed 内部 `response.error` 不是 HTTP 错误封套，其 code 按锁定官方 Schema 的枚举映射，如 server_error/rate_limit_exceeded；不能把任意内部 upstream_timeout 错误码直接填进去。内部固定码可放在安全 message 中；字段结构和终态分别做快照测试。[O7]

Chat/Gemini 某些 SDK 或 UI 可能只表现为流结束，而没有好看的错误弹窗。这部分需要实测并记录为部分兼容；服务端成功终止标记的缺失必须可被契约测试识别。对绝不能接受“普通 EOF 被误认为成功”的调用方，优先使用能检查终止事件的 Responses/Anthropic 流，或非流式调用。[G8]

上游重试固定为 0，不自动请求备用供应商。客户端自己重试是新 HTTP 请求。对于原请求携带 JSON Schema/强制工具的情况，若指定的 frog/preset 备用不满足约束，仍返回原规范 API 错误，不能以失败策略绕过约束校验。

上游调用使用自己的子 context；子超时后执行备用时，不能错误地把整个入站请求 context 一并取消。总时限 context、客户端取消和单次上游子时限须分别处理。

### 7.5 单工具 fixture 的有限状态

v1 工具模拟上限：一次标准 function call、一组完整 JSON object 参数、一次工具结果回传、一次固定最终文字。先由完整上下文确定本轮为“发起调用”还是“回传后收尾”，再检查本轮工具选择。发起调用时，声明可含多个函数，但本 fixture 的函数必须存在、arguments 符合声明 Schema、tool_choice 允许调用；未声明、强制另一名称或 none/NONE 禁止调用均在准入前返回 400。

首次命中 fixture：返回唯一 call ID、工具名称和固定参数。第二次请求通常包含原用户任务、assistant tool call 和 tool result；规则仍由同一最新语义用户任务命中。随后 fixture 执行器只检查：

1. 最新语义用户消息之后，上一个适用 assistant 回合确实只有本 fixture 的一个调用。
2. 工具结果位于该调用之后，call ID 相等；没有新用户消息插入或另一个未完成调用。
3. 没有重复、错序、多工具结果；Gemini 无 ID 时只有一个无歧义的 name 可配对。

满足条件且当前选择为 auto/none（Gemini AUTO/NONE）时，返回配置的 final_text。若本轮仍 required/any/ANY 或 forced named tool，则固定文字违反约束，返回 400 fixture_constraint_conflict；示例第二轮显式切换 auto/none。收尾轮不要求重新声明已经完成的工具，关联由历史调用和当前结果验证。不分析结果正文，不访问外网，不执行工具。孤立/重复/错误 ID/多调用返回 400 `unsupported_tool_sequence`。Call ID 可包含 fixture 标识与随机部分，但不是鉴权凭据；验证依赖客户端本次提供的上下文，不保存服务端会话。第三次只重放同一上下文会得到同一收尾动作，不无限再次发工具。

真实 AI 分支接收客户端回传的受支持工具上下文并转发，由上游生成下一步；FakeAPI 同样不执行工具。完整自动编程和无限循环不属于这项 fixture 兼容承诺。

### 7.6 Key、数据库与额度数据

Key 形如 `fk_<public-id>_<32-byte-random-secret>`。secret 由 crypto/rand 生成，base64url 无 padding。SQLite 只存 SHA-256 摘要、公开 id、标签、期限和计数，验证摘要采用恒定时间比较。创建时原文只输出一次，show/list 不能恢复。高熵随机 Key 不当作人类口令，因此不增加密码哈希框架。

推荐三张主表及迁移表：

| 表 | 字段 |
|---|---|
| api_keys | id PK、secret_hash BLOB、label、tags_json、rule_group、created_at_ms、expires_at_ms nullable、revoked_at_ms nullable、total_limit nullable、ai_limit nullable、rpm nullable、max_concurrent |
| key_usage | key_id PK/FK、total_used、ai_used；非负整数 |
| rate_windows | key_id PK/FK、minute_start_ms、accepted_count；每 Key 一行，滚动替换 |
| schema_migrations | version PK、applied_at_ms |

Key policy 是**创建/显式更新模板**，复制成 DB 元数据；修改 YAML policy 不自动改变已有 Key。`keys update --policy ...` 可重新应用限制但不重置 usage。null 表示无限，0 表示禁止；未设置最大并发时取 policy/default，必须是正整数。

计数和有限额度使用非负 int64；数据库 CHECK 约束维持 total_used≥ai_used≥0。自增达到整数边界时拒绝准入并返回固定内部错误，不能溢出或让 SQLite 隐式转成浮点数。reset-usage 同时清零该 Key 的总/AI次数及当前速率窗口，操作含义要在 CLI 帮助中明示。

不存每次请求账本、不保存 response ID 对应正文，不加入账户/订阅/账期表。使用 SQLite WAL、foreign_keys=ON、synchronous=FULL、每连接 busy_timeout 默认 2s；服务 DB pool 起始单连接即可，读改操作短事务。CLI 使用独立连接，依靠 SQLite 锁保持一致。[T8][T9][T10]

服务取得数据库旁文件锁，防止同库启动第二实例；进程崩溃锁由 OS 释放，不依靠 PID 文件猜测。CLI 管理仍可并发。数据库不得放在网络文件系统或多个容器 replica 间共享；水平扩容留到后续。

### 7.7 原子准入和计数语义

定义：

> 总请求额度统计“获准执行的生成请求”；真实 AI 额度统计“获准进入上游调用阶段的尝试”。一次入口 HTTP 请求最多总额度 +1，AI 额度 +1。所有已准入请求不退款。

获得并发位后执行 `BEGIN IMMEDIATE`，重新读取 Key 的撤销/到期/限制，检查本次是否 AI、总额度、AI 额度及固定分钟窗 RPM；全部通过才同时更新 usage 和 rate_windows，然后提交。**不先单独扣总量，再检查 AI 额度。** 任一检查失败整体 rollback。[T8]

| 情况 | 总请求 | AI 请求 | RPM |
|---|---:|---:|---:|
| 参数/鉴权/模型/能力预检错误 | 0 | 0 | 0 |
| 到期/撤销/额度不足/限速/并发位拒绝 | 0 | 0 | 0 |
| 取消导致事务未提交 | 0 | 0 | 0 |
| 预设获准执行，成功/内部失败/超时/取消 | +1 | 0 | +1 |
| AI 获准执行，成功/上游失败/超时/取消 | +1 | +1 | +1 |
| AI 失败后成功返回备用预设/奶蛙 | +1 | +1 | +1 |
| SSE 已开始后断开或失败 | 保持已扣 | 保持已扣 | 保持已扣 |
| 客户端重新发起/SDK 重试 | 新请求重新准入 | 根据新路由 | 根据新准入 |
| 模型目录、健康检查、本地 CLI 检查 | 0 | 0 | 不计生成 RPM |

若进程在提交后、实际网络包发出前崩溃，已提交的 AI 次数仍保留。这统计本地准入尝试，不保证与上游账单调用数逐条相等；不为缩小这类歧义引入分布式事务/退款账本。SQLite commit 失败则不执行上游；返回 503/500 并释放并发位。

取消与提交竞态以数据库实际提交为准：取消已经发出，但提交先完成，仍计一次；提交前检查 context，事务回滚则零次。I/O 故障使提交结果无法确认时不发上游请求，恢复后以 DB 中的计数为准。Key 标签/规则组在本次鉴权时快照固定；管理更新影响之后鉴权的请求，准入事务仍重新检查最新撤销、到期、额度和 RPM。

默认终身总额度，无每日/每月自动重置。RPM 使用 UTC 整分钟窗口，边界可能允许前后两分钟的突发量，这是已知固定窗口语义；并发限制约束同时占用资源。当前分钟的计数持久化，重启不清空。服务器时钟回退时不把已记录窗口倒退或清零，等待时钟追上或显式管理修复；到期判断依赖正确的服务器 UTC 时钟，测试使用注入时钟。

`now >= expires_at` 为过期。撤销或到期只阻止新准入，已准入流允许继续到取消/结束/硬时限，避免边界时刻的半截输出。活跃并发位只在内存中，重启后恢复为零；Key、额度、撤销、期限、rate window 和 DB schema 持久保留。

Key 并发管理按稳定 key_id 保存受 mutex 保护的活动数，不使用永远不更新容量的固定 channel。每次准入读取当前限制；CLI 调低限制到活动数以下时，现有请求继续，新请求拒绝直到活动数低于新值；调高后后续请求即可使用新值。不通过重建计数器丢失已占用槽位。服务级和上游级并发同样采用先检查、占位、失败即释放的固定顺序。

### 7.8 请求日志和隐私保障

每个生成请求结束时只允许这四个字段，默认 JSONL 写 stdout，可配置文件输出：

```json
{"time":"2026-10-12T08:31:59Z","model":"fakeapi-frog","duration_ms":842,"rule":"default-frog"}
```

time 为接收时间，duration_ms 用单调时钟计算；model 只能是目录中已验证的展示 ID，未知/非法模型为 null；rule 只能来自配置中的 ID，未完成匹配为 null。AI fallback 日志仍记录命中的 AI rule，不改成全局 default-frog。

日志不增加 status、IP、URL/query、User-Agent、Key ID/原文、seed、输入摘要、错误原文、正文、思考、图片或工具结果。解析失败也只写能安全获得的四字段。健康和目录不混入生成请求日志。

实现采用仅接收四个参数的专用 logger，禁止将整个 request/response 或 `err.Error()` 交给通用日志函数。标准 HTTP access log、上游 SDK DEBUG、HTTP dump、panic 原始值均关闭。服务生命周期日志可在 stderr 输出配置字段路径、启动/停止和固定内部错误码，但不能另建一份逐请求详细诊断日志绕过约束。

若请求日志写入失败，服务继续返回已经获得的结果，stderr 最多报告固定 `E_REQUEST_LOG_WRITE`，不重复执行请求；生产文档说明日志为 best effort，额度以 DB 为准。file 模式提供有限轮转（默认 10 MiB × 3），避免磁盘持续增长；stdout 模式由运行环境负责保留和轮转。

隐私测试向用户/系统/历史/工具文本、思考、图片、各 Key 载体、未知 model、上游错误正文放入不同 canary，覆盖成功/失败/取消/panic；搜索 server stdout/stderr、日志、SQLite/WAL，不得出现 canary。显式 Key 创建命令向操作者显示新 Key 是管理交付，不算请求日志，测试须将这条授权输出与服务器日志分开。

## 8. 分阶段实施任务

### P0 契约冻结与工程骨架，1–2 日

- **依赖：**本文和官方协议来源。
- **主要文件：**go.mod/go.sum、internal/core、schemas、tests/contract、docs/decisions、SDK lockfiles、README 初版。
- **任务：**核对四种协议相关字段/事件；固定 Go 与依赖版本；建立规范请求/响应类型；选择模型 ID、错误码、字段白名单和版本策略；记录研究日期和来源；建立可构建 CLI 外壳。
- **交付物：**可编译的 version/help；架构决策记录；每协议最少一个成功/错误/流快照；完整测试版本清单。
- **验收：**go test ./... 能运行；仓库不含密钥/数据；契约禁止范围与本文一致；无需真实网络就可读 fixture。
- **测试：**契约 Schema 静态检查，依赖许可检查，Windows/Linux/macOS 构建试跑。

### P1 配置、规则与素材，3–4 日

- **依赖：**P0 类型与配置契约。
- **主要文件：**internal/config、rules、assets、preset，assets/manifest.yaml，examples，schemas/config.v1.schema.json。
- **任务：**实现严格 YAML 校验、路径/环境引用、rule compile/match/explain；四协议最新文本提取；素材创作和导入；固定/随机文本、有限算术、JSON/tool fixture 数据定义；init 导出模板。
- **交付物：**可用 init/config check/rules explain/assets lint；6 张奶蛙及全部来源声明；纯预设/混合配置；规则正反例列表。
- **验收：**所有引用可解析；正则错误带字段/行号；“你好+长任务”不误匹配；图片-only 不读历史；精确测试关闭思考；随机 seed 独立于并发顺序。
- **测试：**规则表驱动测试、配置负例、素材 UTF-8/宽度/换行测试、Schema fixture 验证；比对生成配置与源码 Schema。

### P2 Key 与 SQLite 原子额度，2–3 日

- **依赖：**P0/P1 配置、错误模型。
- **主要文件：**internal/auth、store/sqlite、quota、cli/keys，migration SQL，tests/integration/quota。
- **任务：**Key 创建/查看/撤销/修改、expiry、policy 快照；SQLite WAL/FULL/短事务；总与 AI 额度、RPM、并发限制；单服务实例文件锁；备份与恢复契约。
- **交付物：**全部必要 Key CLI；可直接测试的 Admit 接口和 DB schema。
- **验收：**并发下绝无超发；AI 配额不足时总量不变；重启保留计数/撤销/速率窗口；Key 原文不可从 show/DB 恢复；CLI 与服务并发管理可用。
- **测试：**100 并发争抢总额度 50；AI 额度 10 与总额度组合；注入时钟验证边界；commit 前后取消屏障；崩溃重启/文件锁/SQLite busy；测试时提高并发上限、关闭 RPM，避免把容量拒绝误判为额度正确。

### P3 Chat Completions 与预设 SSE，4–5 日，发布 v0.1

- **依赖：**P1/P2。
- **主要文件：**internal/server、protocol/openaichat、pacing、requestlog，tests/contract/chat，tests/sdk。
- **任务：**模型目录、JSON 响应、Chat SSE、usage/finish、三个节奏、完整取消清理；单工具往返/JSON fixture；严格四字段日志；容量与准入编排。
- **交付物：**纯预设可本地运行；OpenAI Python/TS Chat 测试通过；基础 Dockerfile 可构建。
- **验收：**所有普通/工具/JSON/错误/限速示例运行；SSE 拼接与非流式正文一致；含 usage 终片正确；停止后资源释放；默认奶蛙可在等宽框显示。
- **测试：**协议快照、首片/节奏 fake clock、网络取消/慢读、stop 跨片段、Unicode、日志 canary；Cherry Studio 和 Open WebUI Chat 首次人工验收。

### P4 OpenAI 兼容上游与混合分流，3–4 日，发布 v0.2

- **依赖：**P3。
- **主要文件：**internal/upstream/openaichat、capabilities.go、server/pipeline，tests/mockupstream，examples/hybrid。
- **任务：**多个地址/真实模型/target、四条可开关 AI 规则、完整对话转发、能力校验、重写 model/鉴权；超时/并发/预提交门槛；三个错误策略；上游重试为零。
- **交付物：**混合配置可运行；测试模拟上游能控制状态、片段、延迟和断开；无匹配规则时零上游调用。
- **验收：**仅指定 Key/规则路由 AI；鹈鹕原请求及完整历史保留；未配置/配置错/调用失败/流中失败行为分明；所有已准入 AI 尝试仅计一次。
- **测试：**模拟上游断言请求结构与目标 model（仅测试进程内），200/401/429/5xx/坏 JSON/坏 SSE/首片超时/半路 EOF/取消；两种备用回复与约束冲突；不使用真实 Key。

### P5 Responses 无状态核心，3–4 日，发布 v0.3

- **依赖：**P4 通用事件与上游转换。
- **主要文件：**protocol/openairesponses、upstream/translate、tests/contract/responses、SDK tests。
- **任务：**string/list input、instructions、assistant output 重传、文本和 function items、reasoning summary、完整流事件、completed/incomplete/failed；明确拒绝有状态与后台能力。
- **交付物：**OpenAI Python/TS 的 Responses create/stream/最终对象 helper通过；手动重传历史可两轮对话；Codex CLI 固定版本探索记录。
- **验收：**事件 ID/index/sequence 全部一致，最终对象与累积文本一致；函数参数流可累积；previous_response_id/store:true 返回明确错误；没有伪造 encrypted_content。
- **测试：**快照、完整 output 重放、token 长度终态、每个中间事件位置注入错误/取消、SDK 最终累积器；Codex 只做本计划限定范围。

### P6 Anthropic Messages 核心，3–4 日，发布 v0.4

- **依赖：**P4/P5 的跨协议转换与事件测试设施。
- **主要文件：**protocol/anthropic、rules/extract、上游 translator、tests/contract/anthropic。
- **任务：**独立基址/模型分页、两种 Key header、版本、Messages 文本/图片/标准函数、SSE block 生命周期、错误；JSON 格式；工具-only user 识别；思考签名边界。
- **交付物：**Anthropic Python/TS create/stream/final-message/models 测试通过；Cherry Studio Anthropic 记录。
- **验收：**message_start→blocks→message_delta→message_stop；累计 usage 不被逐块相加；无伪造签名；跨 Chat 流式估算 usage 明示；根 /v1/models 仍 OpenAI 格式。
- **测试：**工具嵌套文本隔离、图片-only、新旧 Key header冲突、版本错误、模型分页、JSON output_config、错误 SSE；Claude Code 实验范围单列。

### P7 Gemini Generate Content 核心，3–4 日，发布 v0.5

- **依赖：**P6 的稳定通用转换与 fixture。
- **主要文件：**protocol/gemini、rules/extract、tests/contract/gemini、SDK tests。
- **任务：**v1beta 路径和模型分页、Key header/query、contents/parts、SSE 增量、usage、thought、functionCall/functionResponse、JSON 新旧格式归一化。
- **交付物：**google-genai Python 与 @google/genai Node 的指定调用集通过；Cherry Studio Gemini 记录。
- **验收：**不混入 Interactions 事件；没有 [DONE]；functionCall.args 完整；返回 finishReason；query Key 零日志泄露；流式中途错误的 SDK 行为准确登记。
- **测试：**REST 新旧 JSON 字段冲突、SDK text 与 thought分离、function response-only 用户、inline 图片、无效 file URI 转换、显式 abort；只支持 alt=sse 的错误用例。

### P8 全量验收、部署与开源发布，2–4 日，发布 v1.0

- **依赖：**P0–P7 全部指定发布门槛。
- **主要文件：**Dockerfile/compose、GitHub workflows、README/README.en、docs、CHANGELOG、LICENSE、第三方声明和兼容报告。
- **任务：**多平台二进制/容器、两套示例、离线教程、性能测量、素材视觉复核、全部 SDK 矩阵、主流客户端人工验收、升级/备份/回滚验证。
- **交付物：**带校验和的 release 包、GHCR 镜像、源代码、协议/配置文档、固定版本兼容记录、发布检查清单。
- **验收：**干净 Windows 与 Docker Linux 环境从 README 可完成 init→创建 Key→调用→撤销→重启；两种模式都可离线用 mock 上游验证；无重大协议/额度/隐私缺陷。
- **测试：**完整 CI、容器 smoke、升级前后数据一致性、Windows/Ubuntu/macOS 原生 smoke；提交发行标签前重新跑锁定 SDK 测试。

Codex 应按阶段完成相关测试与文档同步后再继续，不到最后才补测试。每阶段形成小提交和阶段结果；受阻时先完成无依赖子任务，明确记录契约差异，不通过删除验收项换取通过。

## 9. 测试方案和兼容验收

### 9.1 自动化测试层次

1. **单元测试：**纯函数规则/规范化、配置、模型映射、usage、停止串、Schema、种子/节奏计算。
2. **契约测试：**四协议请求/响应/错误/SSE 数据结构、事件顺序、字段 null/缺省、SDK最终对象。
3. **集成测试：**临时 SQLite + HTTP test server + controllable upstream；额度/并发/取消/崩溃/备用行为。
4. **SDK 测试：**锁定 Python/Node 官方 SDK；对 localhost 调用，无真实 AI。
5. **客户端人工验收：**固定版本、平台、Provider 和具体能力，不把人工 UI 结果等同于所有客户端保证。

测试 fixture 自行创作，避免把私人对话/真实 API Key 录入仓库。上游 mock 放在 tests，不成为默认公网 endpoint；命令 `go run ./tests/mockupstream/cmd --listen 127.0.0.1:9099` 仅用于开发和离线示例。

### 9.2 必须覆盖的用例

| ID | 场景 | 验收断言 |
|---|---|---|
| R01 | 四协议的 string/多 text block | 提取结果与约定的换行一致 |
| R02 | 历史/system/tool 含触发词 | 不影响当前文本路由 |
| R03 | 最新图-only/空文字/无 user | 不回溯旧文字；fallback 或合法错误 |
| R04 | 纯你好 vs 你好+长任务 | 仅纯问候触发 greeting |
| R05 | 精确/keyword/regex/any/all/none | 与文档真值表一致；非法 RE2 配置拒绝 |
| R06 | 优先级/禁用/模型/可信 Key tags | 首个正确规则唯一命中；客户端自报标签无效 |
| R07 | 简单算术/除零/边界/不支持表达式 | 有限 grammar；无 eval/溢出/意外任意计算 |
| R08 | 鹈鹕中英文明确指令和负例 | 不将普通 SVG 或含关键词长文误分流 |
| R09 | 同 seed 并发/重启/取消 | 正文选择和逻辑节奏相同；请求 ID 可不同 |
| P01 | 四协议 JSON 成功 | 所需字段、type、model、usage、结束原因符合本项目契约 |
| P02 | 四协议模型目录/详情/分页 | 不串用格式；未知/非法 ID 不回显未验证值 |
| P03 | SSE 正常、零正文和多片 | 完整起止、合法 index、无重复文本/usage |
| P04 | SSE 任意 TCP 分片/CRLF/多行/大事件 | 解析可靠；超限有界失败 |
| P05 | Responses output 手动重放 | 第二轮保留完整支持的历史，SDK helper可用 |
| P06 | stop 跨两个 chunk、max token | 截断正确，无 UTF-8/grapheme破坏；结束原因真实 |
| P07 | 模拟思考四模式与显示开关 | 模拟标签明确，JSON/严格输出不污染，无伪造签名 |
| P08 | 图片+文/图-only/无效base64/MIME | 必要字段解析；不发生外部图片下载 |
| P09 | tool fixture 首轮和一次回传 | ID/name/参数正确；收尾一次；服务器零工具执行 |
| P10 | 错序/错 ID/重复结果/forced tool | 明确400，不重复调用/不返回违规普通文本 |
| P11 | JSON schema匹配/不匹配/外部ref | 纯JSON；不匹配400；禁止外部网络/文件加载 |
| P12 | 不支持 n>1、后台/Files/unknown字段 | 对应协议错误，非默认框架422 |
| P13 | Chat usage 有/无 reasoning_tokens、非法拆分 | visible=C−R；总数不重复；混合估算路径有确定断言 |
| P14 | Google SDK 的双字段 Pydantic Schema | description/propertyOrdering 不被误拒；parsed 或模型验证成功 |
| P15 | Gemini 首段→长停顿→末段 | Python sync/async 与 JS 均可读；没有 comment/event/id/retry 行 |
| U01 | 未命中 AI | mock upstream 请求计数为0 |
| U02 | 四个 AI 开关和标签 | 只对指定规则/Key访问指定目标 |
| U03 | 完整上下文与真实模型映射 | 用户/系统/历史/工具顺序保留；客户端Key不转发 |
| U04 | 目标不支持图片/工具/Schema | 预检失败，不调用上游、不扣额度 |
| U05 | 连接失败/401/429/500/坏响应 | 安全规范错误，状态码不误导入口Key |
| U06 | 首片/idle/total超时 | timer结束、body关闭、并发位释放 |
| U07 | 提交前的 preset/frog备用 | 本次只扣一次AI；无重新路由；仍服从约束 |
| U08 | 提交后失败/半途EOF | 不发送成功终止标记，不拼接奶蛙；SDK行为有记录 |
| U09 | 上游重试 | 默认严格一次尝试 |
| U10 | role-only→error 与 role-only→正常空结束 | 前者未提交可备用，后者合法成功；非空正文后错误不可备用 |
| U11 | 子超时与 total 已耗尽 | 只有剩余预算可备用，不能重启 total 定时器 |
| Q01 | Key创建/摘要/查看/撤销 | 无原文持久化；已撤销Key不能新准入 |
| Q02 | expiry 前一毫秒/等于/之后 | now>=expires过期，时区解析正确 |
| Q03 | 总50/AI10的并发争抢 | 成功准入严格不超过限额，失败不半扣 |
| Q04 | 固定分钟窗和重启 | 边界按文档；重启不归零；时钟回退不重置 |
| Q05 | 准入前后取消/流中断 | 根据提交线性化点计数；无退款 |
| Q06 | 提交后进程退出、SQLite busy | 持久计数正确；busy未提交时不发上游 |
| Q07 | policy更新/现有Key/调低限额 | 模板不自动覆盖；调低后新准入按当前limit拒绝 |
| Q08 | 2条活动流时将并发限制3调到1 | 新请求拒绝，旧请求继续；活动数降到0后重新接受 |
| C01 | 取消/慢客户端/写失败 | 在本机取消事件可见后1s内释放资源（测试上界2s） |
| C02 | 100轮连接/取消/超时循环 | 活跃计数归零，goroutine/FD无随轮数持续增长 |
| L01 | 全路径canary | server stdout/stderr/log/DB/WAL无内容和Key原文 |
| L02 | 日志结构/未知model注入 | 每行字段集合恰为四项，model只来自目录或null |
| A01 | 全部内置素材许可与宽度 | 无来源待定项、ASCII/换行符合规范 |
| A02 | 字体/客户端/窄窗显示 | 围栏等宽；40列素材不意外折行；截图仅使用人工fixture |
| D01 | init、本地、Compose全新启动 | 不改代码完成配置、创建Key和四协议调用 |
| D02 | 备份、迁移、回滚 | Key/额度/撤销保留；新schema不被旧二进制误打开 |

对关键规则/额度/协议编码模块以 Go 工具报告的语句覆盖率 ≥85% 为参考门槛，关键条件分支则由上述用例逐项覆盖；不把语句覆盖数字说成自动测得的分支覆盖。不为每个 getter 写镜像测试。取消/时间测试使用 fake clock 和屏障，真实网络测试只设少量宽松上界，避免靠 sleep 猜时机。额度争抢测试需提高其它容量限制，使预期的额度拒绝为 429，不能让提前的 503 掩盖并发计数问题。

### 9.3 可控模拟上游

模拟上游必须支持脚本化设置：状态码、安全/含 canary 错误正文；正常 JSON；多个 SSE chunk；首片延迟；片间停顿；role-only 后错误；中途断开；无终止标记；损坏 JSON；过大事件；缺失 usage；正确/错误 tool ID；已提供 reasoning_content。

测试通过构造器在进程内传入脚本，不按生产请求正文里的特殊关键词触发故障。CLI demo 只启用固定人工 fixture。模拟上游可在测试内存捕获请求以断言完整上下文，但不启用生产日志，不将捕获到的实际用户内容写入报告。

SDK 自动重试在主测试中关闭；另做专门用例验证重试导致多个 HTTP 准入与额度消耗。Google Python 的自动函数执行关闭，提交函数声明/结果，避免 SDK 替服务端执行工具而掩盖协议缺陷。[S1][S5]

### 9.4 官方 SDK 验收矩阵

| SDK | 配置方式 | 发布必须通过的能力 |
|---|---|---|
| OpenAI Python `openai` | api_key + base_url `/v1`，max_retries=0 | Chat/Responses create、原始stream与最终对象 helper、models、usage、JSON、单工具往返、错误、显式 close/取消 |
| OpenAI TS `openai` | apiKey + baseURL `/v1`，maxRetries=0 | 同上；尤其 Responses事件累积、最终output与AbortController |
| Anthropic Python `anthropic` | api_key + base_url `/anthropic` | messages.create、messages.stream/get_final_message、models分页、content blocks、JSON、错误、context manager退出 |
| Anthropic TS `@anthropic-ai/sdk` | apiKey + baseURL `/anthropic` | messages create/stream/finalMessage、工具参数累积、错误、stream controller abort |
| Google Python `google-genai` | api_key + HttpOptions(base_url=root, api_version=v1beta) | models list/get、generate_content、generate_content_stream、chat两轮、parts/thought、JSON、函数、错误、async关闭 |
| Google TS `@google/genai` | apiKey + httpOptions(baseUrl=root, apiVersion=v1beta) | generateContent/Stream、模型、text与parts、单工具、JSON、usage、config.abortSignal |

六个 SDK 在协议相关 PR 的 CI 和 release CI 中均运行；P0 固定具体稳定版本并写入 lockfile，不在每次 CI 安装 latest。基本矩阵采用一种 Python 与 Node LTS，按季度或依赖升级 PR 重新测最新版。测试依赖不进入 Go 运行镜像。[S1][S2][S3][S4][S5][S6]

测试运行时推荐 Python 3.13 与 Node.js 24，P0 根据当时 SDK 支持范围锁定补丁版本。当前文档核对只形成设计和验收目标，六个 SDK 的实际通过记录须由对应实现阶段产出。

### 9.5 最值得验收的客户端

| 对象 | 等级 | 明确验收范围 |
|---|---|---|
| Cherry Studio | v1 必验，Windows 主平台 | 四种自定义 endpoint 分别配置；模型发现/手动ID；普通聊天；SSE/停止；Markdown奶蛙；图片+文字；portable/native思考实际显示 |
| Open WebUI | v1 必验，Linux Docker | OpenAI-compatible 的models/Chat、连续聊天、SSE/停止、字符画、JSON fixture；Responses入口单列实验性结果 |
| aider | 次优先固定版本实测 | OpenAI-compatible，ask 模式文本/流/取消；编辑能力仅在另有匹配其编辑格式的fixture且实测后声明 |
| Cline | 次优先固定版本实测 | OpenAI Compatible接入、标准工具声明接收、文本/SSE/取消、一次明确的工具调用展示；固定fixture可独立测试收尾 |
| Codex CLI | 有条件/探索 | 自定义Provider、Responses HTTP/SSE、单轮文本、完成事件、取消；仅声明通过的标准函数fixture，不承诺完整代码任务 |
| Claude Code | 有条件/实验 | 自定义Anthropic基址；指定版本非代理文本/流/取消；可选一次标准工具fixture；不把beta/thinking/自动压缩列完整 |

Cherry Studio 当前文档列出 OpenAI、Responses、Anthropic、Gemini 自定义端点，并存在自动补全路径行为。应记录它实际生成的路径，不能给所有 Provider 机械填同一个 `/v1` URL。Open WebUI 的 Responses 接入仍为实验能力，其原生 Chat 连接先验收。[C1][C2][C3][C4]

当前 Codex 文档要求 Responses，自定义 `wire_api="chat"` 的旧配置不适合作为验收方案。测试关闭 WebSocket和网络搜索，使用本地独立测试配置及临时目录；若客户端默认携带 v1 不支持的自由格式工具/encrypted reasoning/compaction，应登记具体阻断条件，不能将400当成全兼容。Claude Code的 Messages `?beta=true`、可选count_tokens、版本特定工具与思考字段也需要单独记录。[C7][C8]

**编程客户端能力分级：**T0连通/模型；T1文本问答/流/取消；T2标准单函数调用与回传；T3真实文件编辑、终端命令、完整Agent循环。FakeAPI v1 核心承诺到协议/SDK的 T2；各编程客户端只公布实际通过的 T0/T1/T2 组合，T3 暂缓。客户端会改写请求、插入任务模板，可能使严格的 greeting/fixture matcher不命中；测试用配置须针对已核实的请求格式，不扩大生产问候规则来迎合客户端。

兼容记录每行包含：FakeAPI版本、客户端/SDK版本、OS、Provider类型、实际路径、配置文件、模型发现、非流式、流式、结束/取消、思考可解析/可见、JSON、工具往返、结果与已知限制。发布前的空白项写“待验证”，不能用“兼容全部客户端”补齐。

### 9.6 运行指标和实际验证范围

支持目标：Windows 11 amd64、Linux amd64/arm64、macOS arm64/amd64。主门槛是 Windows 11 原生 amd64、Ubuntu 24.04 amd64、macOS arm64 原生；Linux arm64 在 CI runner或QEMU做镜像smoke，macOS amd64若只有交叉编译须标“已构建、待原生验证”。不把交叉编译成功写成所有平台实测成功。

P8 记录冷启动时间、空闲 RSS、默认和最大请求下峰值内存、二进制与镜像大小、instant吞吐和取消释放耗时。参考测量环境为2 vCPU/1 GiB Linux容器；用128字符人工文本、64并发测试时临时调整容量上限，分别测预设和mock上游。不设置未经基准确认的营销数字，也不以低配环境短时波动作为错误的性能回归信号。

## 10. 部署、开源发布、升级和风险

### 10.1 Windows 本地启动契约

以下命令是**实施完成后的 README 验收脚本**，当前交付不代表这些项目文件已在 Windows 创建。源码实施目录固定为 `D:\Projects\FakeAPI`；普通部署者从 GitHub Releases 下载对应二进制即可，无需 Go。

```powershell
Set-Location D:\Projects\FakeAPI
go mod download
go build -trimpath -o .\bin\fakeapi.exe .\cmd\fakeapi
.\bin\fakeapi.exe init --template preset --dir .\runtime\preset
.\bin\fakeapi.exe config check --config .\runtime\preset\fakeapi.yaml
.\bin\fakeapi.exe keys create --config .\runtime\preset\fakeapi.yaml --policy demo --label local --expires-in 30d
.\bin\fakeapi.exe serve --config .\runtime\preset\fakeapi.yaml
```

创建命令显示的入口 Key 由操作者保存；将它填入客户端即可。`init` 导出的配置使用 `assets/manifest.yaml`、`data/state.sqlite` 等相对本配置目录的路径。仓库中的 examples 则使用 `../../assets/manifest.yaml` 和 `../../data/state.sqlite`。两种路径在初始化时由同一模板生成，不能直接复制 examples 后保留错误的 `../../`。

Linux/macOS 使用相同子命令，换成 `./fakeapi` 和 POSIX 路径。测试纯预设不需设置任何上游环境变量。默认配置监听本机；部署者需要局域网访问时显式选择监听地址，并在自己的反向代理上配置 TLS。v1 不捆绑反向代理或域名服务。

### 10.2 混合配置的离线启动

实施 P4 后，开发者先在单独终端运行受控上游：

```powershell
Set-Location D:\Projects\FakeAPI
go run .\tests\mockupstream\cmd --listen 127.0.0.1:9099 --scenario normal
```

在服务终端设置测试环境变量并启动：

```powershell
$env:FAKEAPI_UPSTREAM_BASE_URL = 'http://127.0.0.1:9099/v1'
$env:FAKEAPI_UPSTREAM_API_KEY = 'offline-test-key'
$env:FAKEAPI_UPSTREAM_MODEL = 'mock-fast'
$env:FAKEAPI_UPSTREAM_SVG_MODEL = 'mock-svg'
.\bin\fakeapi.exe init --template hybrid --dir .\runtime\hybrid
.\bin\fakeapi.exe config check --config .\runtime\hybrid\fakeapi.yaml
.\bin\fakeapi.exe keys create --config .\runtime\hybrid\fakeapi.yaml --policy hybrid --label hybrid-local
.\bin\fakeapi.exe serve --config .\runtime\hybrid\fakeapi.yaml
```

`normal` 模拟上游同时处理 Chat 普通/流式请求，响应中带固定测试标识；HTTP 请求中的真实 model 值可被测试断言。测试必须确认：四类 AI matcher 各命中时确实调用 mock；关闭其中一条后只影响该类；普通长任务和无 ai 标签 Key 的请求调用次数为零。

连接真实上游时只替换这四个环境变量，并核实 target 的能力声明。若只想使用一个真实模型，两个 model 环境变量可填相同值。`.env.example` 只提供名称和说明，不内置真实凭据；本机程序不自动读取 `.env`，由 shell 或部署工具显式提供。Docker Compose 可从本地 `.env` 读取其明确引用的变量。

### 10.3 Dockerfile 和 Compose

推荐多阶段构建：官方 Go 镜像构建、CGO_ENABLED=0、最终 scratch 镜像包含一个二进制及 CA 证书。默认素材已嵌入二进制，init 可导出；运行镜像无编译器、包管理器和供应商 SDK。P0 验证所选官方镜像 tag，发布流程固定版本及实际 digest。[T11]

代表性 Dockerfile（实施时加入版本/commit 构建信息和 OCI license/source 标签）：

```dockerfile
# syntax=docker/dockerfile:1
ARG GO_VERSION=1.27.2
FROM --platform=$BUILDPLATFORM golang:${GO_VERSION}-bookworm AS build
ARG TARGETOS
ARG TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w" -o /out/fakeapi ./cmd/fakeapi
RUN mkdir -p /out/data
RUN chown 65532:65532 /out/data

FROM scratch
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /out/fakeapi /fakeapi
COPY --from=build --chown=65532:65532 /out/data /data
USER 65532:65532
WORKDIR /data
EXPOSE 8787
ENTRYPOINT ["/fakeapi"]
```

代表性仓库根目录 `compose.yaml`：

```yaml
services:
  fakeapi:
    image: ${FAKEAPI_IMAGE:-fakeapi:local}
    build:
      context: .
      args:
        GO_VERSION: 1.27.2
    command: [serve, --listen, '0.0.0.0:8787']
    environment:
      FAKEAPI_CONFIG: /config/fakeapi.yaml
      FAKEAPI_STATE: /data/state.sqlite
      FAKEAPI_UPSTREAM_BASE_URL: ${FAKEAPI_UPSTREAM_BASE_URL:-}
      FAKEAPI_UPSTREAM_API_KEY: ${FAKEAPI_UPSTREAM_API_KEY:-}
      FAKEAPI_UPSTREAM_MODEL: ${FAKEAPI_UPSTREAM_MODEL:-}
      FAKEAPI_UPSTREAM_SVG_MODEL: ${FAKEAPI_UPSTREAM_SVG_MODEL:-}
    ports: ['127.0.0.1:8787:8787']
    volumes:
      - ./runtime:/config:ro
      - fakeapi-data:/data
    read_only: true
    tmpfs: [/tmp]
    cap_drop: [ALL]
    security_opt: [no-new-privileges:true]
    restart: unless-stopped
    stop_grace_period: 15s
    healthcheck:
      test: [CMD, /fakeapi, health, --url, 'http://127.0.0.1:8787']
      interval: 15s
      timeout: 3s
      retries: 3
      start_period: 5s
    logging:
      driver: json-file
      options: {max-size: '10m', max-file: '3'}
volumes:
  fakeapi-data: {}
```

单独使用 Docker 时也必须能完成初始化和 Key 创建，不要求宿主机安装 Go。PowerShell 示例：

```powershell
docker compose build
New-Item -ItemType Directory -Force .\runtime | Out-Null
$fakeapiRuntime = (Resolve-Path .\runtime).Path
docker run --rm --user 0:0 --mount "type=bind,source=$fakeapiRuntime,target=/config" fakeapi:local init --template preset --dir /config
docker compose run --rm --no-deps fakeapi config check
docker compose run --rm --no-deps fakeapi keys create --policy demo --label docker-local
docker compose up -d
docker compose exec fakeapi keys list
```

非 root 服务需要可写的 `/data`；首次 named volume 必须继承镜像目录的 UID/GID，并在 CI 实测，不能仅检查 Dockerfile 可构建。使用 bind mount 保存 DB 时，文档提供宿主机权限设置说明；不在服务启动时自动修改整棵宿主目录权限。Linux 上导出配置可使用当前用户 UID/GID，避免 root 所有的配置文件；服务对配置只需读取。

Compose 默认单副本，配置/素材挂载只读、SQLite 独占一个可写卷。修改素材在宿主机完成后重启。request_log 默认 stdout；不能把日志文件默认写进只读 `/config`。`compose down` 不删除数据卷，文档不把 `down -v` 当普通升级命令。

容器混合模式连接宿主机的 mock 时，使用 Docker Desktop 的宿主机地址，Linux 在测试 Compose override 中显式配置 host gateway；容器内 127.0.0.1 不是宿主机。CI 优先将受控上游作为同一测试网络中的服务，不依赖宿主特殊 DNS。

### 10.4 GitHub Actions 与发行物

| 工作流 | 触发和工作 | 验收要求 |
|---|---|---|
| ci.yml | PR/push；gofmt、go vet、单测、集成、race、短时 fuzz、配置/素材/隐私检查 | 核心测试离线；不注入任何真实 AI secret |
| sdk.yml | PR 涉及协议及发布前；安装锁定 Python/Node SDK，启动 FakeAPI + mock | 六个官方 SDK 的阻断用例通过；禁用自动工具执行及非测试重试 |
| release.yml | v* tag；重复验收、交叉构建、压缩包、SHA256、GHCR 多架构镜像 | 只发布通过的版本；README/CHANGELOG/兼容记录与该 tag 一致 |

CI 在 Linux、Windows、macOS runner 上测试 CLI/SQLite；使用架构明确的 runner 标签并记录其 OS 版本，不把 Windows Server runner 当成 Windows 11 手工客户端验收。官方 runner 的架构/标签可能变化，P0 按当时官方列表锁定。[T12]

直接依赖、SDK、Go 工具链和 GitHub Actions 引用固定到经过检查的版本/commit；日常升级通过独立 PR 运行相同回归。依赖安装阶段可以联网，测试执行阶段不得访问真实 AI。Fuzz 在 PR 中限定时长，长时 fuzz 可独立手动运行，不为轻量项目维护长期测试集群。

发行文件：

- `fakeapi_<version>_windows_amd64.zip`。
- `fakeapi_<version>_linux_amd64.tar.gz`、`linux_arm64.tar.gz`。
- `fakeapi_<version>_darwin_arm64.tar.gz`、`darwin_amd64.tar.gz`；分别标注验证程度。
- `SHA256SUMS`、LICENSE、第三方许可、README、示例和素材；二进制的 init 与归档素材必须来自同一源。
- `ghcr.io/<owner>/fakeapi:<version>`，linux/amd64 + linux/arm64；首个稳定版本后维护 `:1`，`:latest` 仅指稳定发行，不指未验收主分支。

发布工作流只在 tag 构建步骤具有 contents/packages 写权限；普通 PR 无发布权限。镜像和归档记录源 commit、license、版本。使用 GitHub 原生生成来源证明可作为发布增强项；不把额外签名基础设施设为首版运行依赖。

### 10.5 配置和数据库升级

配置版本用 `schema_version`，应用版本用 SemVer，数据库迁移版本单独递增。小版本只新增具有明确默认值的可选字段；删除/改变含义的字段需大版本迁移说明。旧配置未知字段/不支持 schema 直接给出安全错误，不自动猜测。

升级步骤：先阅读 CHANGELOG 和兼容说明 → 用旧版本 db backup 生成一致备份 → 保存旧配置 → 停止旧服务 → 用新版本 config check → 执行需要的 db migrate → 启动并运行健康、模型、一次预设生成检查。v1 首次建库可自动执行初始建表；后续改变持久结构的迁移必须明确调用 migrate，并先备份。数据库版本比程序新时拒绝打开。

回滚使用旧二进制、旧配置和迁移前备份；不让旧程序强行打开新 Schema，也不自动删掉计数。备份包含 Key 哈希/元数据/额度/限速窗口，不包含对话正文。迁移失败保持事务回滚；备份路径不可覆盖当前数据库或其 WAL/SHM 文件。

### 10.6 开源发布清单

- 中文主 README：一分钟说明、两种用途、快捷启动、Key、四协议地址、纯预设/混合例、隐私和次数语义、已知兼容边界。
- 英文 README.en.md：相同的核心安装、接口、配置和限制；协议/配置详情链接到对应文档，版本更新同步。
- MIT LICENSE：标准 MIT 文本与项目版权声明；素材来源表与第三方许可单独保留。
- CONTRIBUTING：模块边界、如何加规则/素材/协议 fixture、离线测试命令和 PR 要求；增加供应商前必须说明转换范围。
- SECURITY：报告方式、支持版本；不能填不存在的维护者邮箱。GitHub 仓库建成时配置实际可用的私密报告入口。
- CHANGELOG、Issue/PR 模板：复现需版本/最小人工配置/脱敏结构，明确不要上传真实 Key、对话、图片和数据库。
- examples/preset、examples/hybrid、examples/testing 均 config check 通过；init 导出物与目录路径真实可用。
- 6 张奶蛙、2 份思考、4 条彩蛋、3 个 fixture 已交付并登记来源；等宽、换行、许可与客户端显示复核完成。
- SDK 自动化结果与客户端手工记录包含真实版本、时间、能力和失败原因；没有记录的能力标待验证。
- 全新 Windows、Linux、macOS 安装/启动和规定的平台矩阵通过；Docker 非 root、只读配置、持久卷、重启保留计数通过。
- 日志/DB/WAL canary 零泄漏；没有真实上游 secret、默认入口 Key、开发数据或付费 CI 依赖进入仓库/镜像。

### 10.7 主要技术风险和待验证假设

| 风险/假设 | 影响 | 计划中的处理和完成条件 |
|---|---|---|
| 四协议事件和字段持续变化 | 同名 endpoint 仍可能不能被 SDK 累积 | P0 锁正式类型/版本，每协议独立 fixture，发布前重验；新版本另开兼容 PR |
| Responses 最终对象和 replay 较复杂 | 文本片段能显示但 SDK get_final_response 失败 | P5 覆盖完整生命周期、final.output、无状态重传、失败/incomplete |
| Gemini SSE parser 并非通用 SSE | 标准心跳导致客户端报错 | 固定 data 格式，关闭该协议心跳，Python sync/async + JS 长停顿测试 |
| 思考显示因客户端而异 | 原生面板不出现或签名无法回传 | 默认 portable，native 单列部分支持，不伪造签名，不静默删除必需历史 |
| Chat 上游能力差异 | max token 字段/JSON/tool/developer 不兼容 | 每 target 显式声明，不自动探测；mock 覆盖支持/拒绝路径 |
| 编程客户端使用自定义工具/压缩/存储 | 文本兼容不等于 Agent 可用 | T0–T3 分级；Codex/Claude Code 有条件，T3 暂缓 |
| usage 跨协议不能无损 | 统计重复或先后不一致 | 互斥 visible/reasoning、C−R、估算标记；Anthropic 跨流统一估算 |
| SQLite 写锁和持久化时序 | 高并发超额、崩溃后计数变化 | 短事务、FULL、busy 限时、无退款准入、重启/强杀/并发测试 |
| 慢消费者与上游取消 | goroutine、连接、槽位泄漏 | 有界队列、每写 deadline、子 context、取消负载测试 |
| 模拟思考 + JSON/SVG/tool 冲突 | 输出形状看似成功但不可解析 | fixture 强制无思考、预算/约束预检；备用回复也走约束校验 |
| 字符画字宽及素材许可 | 客户端错位或无法分发 | 原创 ASCII、完整来源表、素材 lint 和固定客户端截图 |
| 隐私与问题诊断冲突 | 调试日志意外保存敏感内容 | 四字段 logger、固定错误码、人工最小 fixture；禁止请求 dump |
| 原生平台无法当场获得 | 交叉构建被误报成实测 | 明确“已构建/模拟运行/原生验证”三种状态；缺证据不宣传已验证 |

这些是实施验证事项，不是需要用户重新决定的产品问题。遇到真实文档与本计划的字段差异时，Codex 应先修正对应契约、配置说明和测试，再实现，不扩大 v1 的功能范围。

### 10.8 给 Codex 的连续实施约束

按 P0→P8 推进；每阶段先阅读前置契约和相应官方来源，完成模块、示例、测试、中文文档，再进入下一阶段。用户代码和已有配置必须保留，不能用“重新生成整个项目”掩盖未理解的差异。

每阶段结束记录：完成文件/行为、执行的测试、未通过项和下一阶段依赖。所谓完成应是可复现的命令成功、协议断言通过、资源关闭和隐私验证通过，不能仅以编译成功或 curl 有文本为准。

不在实现过程中添加 Web UI、账户平台、远程管理后台、Redis、会话正文存储、AI 意图分类或工具执行。原生上游、Responses 状态服务和完整 Agent 支持进入后续里程碑，经独立需求与兼容设计再实施。

## 附录 A：完整纯预设配置

以下是 `examples/preset/fakeapi.yaml` 的代表性完整内容，所有素材 ID 在附录 C 中列明并由 P1 交付。AI 规则全部关闭，不需要上游变量。配置结构是待实施的契约，不能声称当前尚未编写的 FakeAPI 已执行过它。

```yaml
schema_version: 1
server: {listen: '127.0.0.1:8787', max_request_bytes: 8388608, max_response_bytes: 1048576, max_sse_event_bytes: 1048576,
  max_concurrent: 64, max_messages: 256, max_tools: 32, read_header_timeout: 5s, read_body_timeout: 15s, write_timeout: 10s,
  shutdown_timeout: 10s, anthropic_messages_alias: true}
storage: {path: ../../data/state.sqlite, busy_timeout: 2s}
request_log: {enabled: true, output: stdout}
defaults:
  profile: natural
  random: {mode: random, algorithm: pcg_v1, seed: null, allow_request_seed: false}
profiles:
  instant:
    thinking: {enabled: false}
    timing: {first_output_delay: 0ms, thinking_to_body_delay: 0ms, graphemes_per_second: 0, chunk_size: 16,
      jitter: 0ms, pause_probability: 0, pause_duration: 0ms, max_total: 5s}
  natural:
    thinking: {enabled: true, channel: portable_text, text_asset: thinking-natural}
    timing: {first_output_delay: 300ms, thinking_to_body_delay: 300ms, graphemes_per_second: 60, chunk_size: 4,
      jitter: 20ms, pause_probability: 0.02, pause_duration: 100ms, max_total: 30s}
  playful:
    thinking: {enabled: true, channel: portable_text, text_asset: thinking-playful}
    timing: {first_output_delay: 1500ms, thinking_to_body_delay: 1500ms, graphemes_per_second: 24, chunk_size: 4,
      jitter: 120ms, pause_probability: 0.1, pause_duration: 800ms, max_total: 45s}
assets: {manifest: ../../assets/manifest.yaml}
key_policies:
  demo:
    tags: [demo]
    group: default
    expires_at: null
    total_limit: 10000
    ai_limit: 0
    rpm: 60
    max_concurrent: 4
  hybrid:
    tags: [ai]
    group: default
    expires_at: null
    total_limit: 10000
    ai_limit: 100
    rpm: 60
    max_concurrent: 4
  test:
    tags: [test]
    group: testing
    expires_at: null
    total_limit: null
    ai_limit: 0
    rpm: null
    max_concurrent: 16
models:
- id: fakeapi-frog
  display_name: 奶蛙 · 自然
  profile: natural
  created_at: '2026-10-12T00:00:00Z'
  limits: {input_tokens: 32768, output_tokens: 8192}
- id: fakeapi-test
  display_name: FakeAPI · 极速测试
  profile: instant
  created_at: '2026-10-12T00:00:00Z'
  limits: {input_tokens: 32768, output_tokens: 8192}
upstreams: {}
targets: {}
default_target: null
matchers:
  greeting:
    max_chars: 24
    any:
    - {regex: '(?i)^(你好|您好|hi|hello|hey)[!！。.]?$'}
  identity:
    max_chars: 64
    any:
    - {regex: '(?i)^(你是谁|你是什么模型|what model are you|who are you)[?？!！。.]?$'}
  simple-math:
    max_chars: 40
    any:
    - {regex: '^(?:计算\s*)?([+-]?\d{1,6})\s*([+*/-])\s*([+-]?\d{1,6})\s*(?:=\s*\??|\?)?$'}
  decimal-test:
    max_chars: 40
    any:
    - {exact: 9.9和9.11哪个大}
    - {exact: 9.9 和 9.11 哪个大？}
  strawberry-test:
    max_chars: 80
    any:
    - {exact: strawberry 中有几个 r？}
    - {exact: 'How many r letters are in strawberry?'}
  ok-test:
    max_chars: 32
    any:
    - {exact: 只回答 OK}
    - {exact: Reply with OK only.}
  pelican-test:
    max_chars: 100
    any:
    - {exact: 用 SVG 画骑自行车的鹈鹕}
    - {exact: 用SVG画骑自行车的鹈鹕}
    - {exact: 请用 SVG 画骑自行车的鹈鹕。}
    - {exact: Draw a pelican riding a bicycle in SVG.}
  json-test:
    any:
    - {exact: /test/json}
  tool-test:
    any:
    - {exact: /test/tool}
  frog-command:
    any:
    - {exact: /frog}
  milk-command:
    any:
    - {exact: /milk}
  croak-easter:
    max_chars: 80
    any:
    - {keyword: 呱呱}
    none:
    - {keyword: 不要}
  afterwork-easter:
    max_chars: 40
    all:
    - {keyword: 奶蛙}
    - {keyword: 下班}
    none:
    - {keyword: 不要}
  party-command:
    any:
    - {exact: /party}
rules:
- id: fixture-json
  enabled: true
  priority: 1200
  when:
    matchers_any: [json-test]
    models: [fakeapi-test]
    key_tags_any: [test]
    key_groups: [testing]
  action:
    type: json_fixture
    json: {ok: true, source: fakeapi}
    schema:
      type: object
      properties:
        ok: {type: boolean}
        source: {type: string}
      required: [ok, source]
      additionalProperties: false
  behavior:
    profile: instant
    thinking: {enabled: false}
- id: fixture-tool
  enabled: true
  priority: 1190
  when:
    matchers_any: [tool-test]
    models: [fakeapi-test]
    key_tags_any: [test]
    key_groups: [testing]
  action:
    type: tool_fixture
    name: get_weather
    arguments: {city: Guangzhou}
    final_text: 工具测试完成。
  behavior:
    profile: instant
    thinking: {enabled: false}
- id: ai-greeting
  enabled: false
  priority: 1000
  when:
    matchers_any: [greeting]
    key_tags_all: [ai]
  action:
    type: upstream
    on_error: {mode: api_error}
- id: ai-identity
  enabled: false
  priority: 990
  when:
    matchers_any: [identity]
    key_tags_all: [ai]
  action:
    type: upstream
    on_error: {mode: api_error}
- id: ai-tests
  enabled: false
  priority: 980
  when:
    matchers_any: [simple-math, decimal-test, strawberry-test, ok-test]
    key_tags_all: [ai]
  action:
    type: upstream
    on_error: {mode: api_error}
- id: ai-pelican
  enabled: false
  priority: 970
  when:
    matchers_any: [pelican-test]
    key_tags_all: [ai]
  action:
    type: upstream
    on_error: {mode: api_error}
- id: preset-greeting
  enabled: true
  priority: 600
  when:
    matchers_any: [greeting]
  action:
    type: preset
    choices:
    - {text: 你好，我是奶蛙，呱。, weight: 3}
    - {text: 你好，奶蛙已就位。, weight: 1}
- id: preset-identity
  enabled: true
  priority: 590
  when:
    matchers_any: [identity]
  action: {type: preset, text: '我是 FakeAPI 模拟服务，当前展示模型是 {{model}}。这条回复来自本地预设。'}
- id: preset-math
  enabled: true
  priority: 580
  when:
    matchers_any: [simple-math]
  action: {type: arithmetic}
  behavior:
    profile: instant
    thinking: {enabled: false}
- id: preset-decimal
  enabled: true
  priority: 570
  when:
    matchers_any: [decimal-test]
  action: {type: preset, text: 9.9 更大，因为 9.90 > 9.11。, strict_text: true}
  behavior:
    profile: instant
    thinking: {enabled: false}
- id: preset-strawberry
  enabled: true
  priority: 560
  when:
    matchers_any: [strawberry-test]
  action: {type: preset, text: '3', strict_text: true}
  behavior:
    profile: instant
    thinking: {enabled: false}
- id: preset-ok
  enabled: true
  priority: 550
  when:
    matchers_any: [ok-test]
  action: {type: preset, text: OK, strict_text: true}
  behavior:
    profile: instant
    thinking: {enabled: false}
- id: preset-pelican
  enabled: true
  priority: 540
  when:
    matchers_any: [pelican-test]
  action: {type: preset, asset: pelican-svg, render: plain}
  behavior:
    profile: instant
    thinking: {enabled: false}
- id: easter-frog
  enabled: true
  priority: 500
  when:
    matchers_any: [frog-command]
    models: [fakeapi-frog]
  action: {type: preset, asset: frog-party}
  behavior: {profile: playful}
- id: easter-frog-test
  enabled: true
  priority: 501
  when:
    matchers_any: [frog-command]
    models: [fakeapi-test]
  action: {type: preset, asset: frog-party}
  behavior:
    profile: instant
    thinking: {enabled: false}
- id: easter-milk
  enabled: true
  priority: 490
  when:
    matchers_any: [milk-command]
  action:
    type: preset
    choices:
    - {asset: frog-milk, weight: 2}
    - {asset: egg-milk, weight: 1}
- id: easter-croak
  enabled: true
  priority: 480
  when:
    matchers_any: [croak-easter]
  action: {type: preset, asset: egg-croak}
- id: easter-afterwork
  enabled: true
  priority: 470
  when:
    matchers_any: [afterwork-easter]
  action: {type: preset, asset: egg-afterwork}
- id: easter-party
  enabled: true
  priority: 460
  when:
    matchers_any: [party-command]
  action: {type: preset, asset: egg-party}
fallback:
  id: default-frog
  action:
    type: preset
    render: auto
    choices:
    - {asset: frog-idle, weight: 4}
    - {asset: frog-milk, weight: 2}
    - {asset: frog-shrug, weight: 2}
    - {asset: frog-party, weight: 1}
```

`demo` Key 供普通趣味调用；`test` Key 及 `fakeapi-test` 用于显式 JSON/工具 fixture。`hybrid` policy 可以创建带 ai 标签的 Key，但在该配置中 AI 规则关闭，因此仍不会访问上游。

## 附录 B：完整混合配置

以下是 `examples/hybrid/fakeapi.yaml` 的完整内容。它与纯预设使用同一套 matcher/素材；四条 AI 规则各自可关，AI 仅对 ai 标签生效。普通请求依旧命中本地预设或奶蛙。两个 target 可指向同一供应商的不同模型；鹈鹕规则显式选择 primary-quality，其他 AI 规则使用展示模型映射或默认 target。

本例 target 能力是离线 mock 的能力声明；部署者改用真实模型时必须按其实际支持调整，不能把配置声明当自动探测结果。

```yaml
schema_version: 1
server: {listen: '127.0.0.1:8787', max_request_bytes: 8388608, max_response_bytes: 1048576, max_sse_event_bytes: 1048576,
  max_concurrent: 64, max_messages: 256, max_tools: 32, read_header_timeout: 5s, read_body_timeout: 15s, write_timeout: 10s,
  shutdown_timeout: 10s, anthropic_messages_alias: true}
storage: {path: ../../data/state.sqlite, busy_timeout: 2s}
request_log: {enabled: true, output: stdout}
defaults:
  profile: natural
  random: {mode: random, algorithm: pcg_v1, seed: null, allow_request_seed: false}
profiles:
  instant:
    thinking: {enabled: false}
    timing: {first_output_delay: 0ms, thinking_to_body_delay: 0ms, graphemes_per_second: 0, chunk_size: 16,
      jitter: 0ms, pause_probability: 0, pause_duration: 0ms, max_total: 5s}
  natural:
    thinking: {enabled: true, channel: portable_text, text_asset: thinking-natural}
    timing: {first_output_delay: 300ms, thinking_to_body_delay: 300ms, graphemes_per_second: 60, chunk_size: 4,
      jitter: 20ms, pause_probability: 0.02, pause_duration: 100ms, max_total: 30s}
  playful:
    thinking: {enabled: true, channel: portable_text, text_asset: thinking-playful}
    timing: {first_output_delay: 1500ms, thinking_to_body_delay: 1500ms, graphemes_per_second: 24, chunk_size: 4,
      jitter: 120ms, pause_probability: 0.1, pause_duration: 800ms, max_total: 45s}
assets: {manifest: ../../assets/manifest.yaml}
key_policies:
  demo:
    tags: [demo]
    group: default
    expires_at: null
    total_limit: 10000
    ai_limit: 0
    rpm: 60
    max_concurrent: 4
  hybrid:
    tags: [ai]
    group: default
    expires_at: null
    total_limit: 10000
    ai_limit: 100
    rpm: 60
    max_concurrent: 4
  test:
    tags: [test]
    group: testing
    expires_at: null
    total_limit: null
    ai_limit: 0
    rpm: null
    max_concurrent: 16
models:
- id: fakeapi-frog
  display_name: 奶蛙 · 自然
  profile: natural
  created_at: '2026-10-12T00:00:00Z'
  limits: {input_tokens: 32768, output_tokens: 8192}
  target: primary-fast
- id: fakeapi-test
  display_name: FakeAPI · 极速测试
  profile: instant
  created_at: '2026-10-12T00:00:00Z'
  limits: {input_tokens: 32768, output_tokens: 8192}
  target: primary-fast
upstreams:
  primary:
    kind: openai_chat
    base_url: {env: FAKEAPI_UPSTREAM_BASE_URL}
    api_key_env: FAKEAPI_UPSTREAM_API_KEY
    max_concurrent: 4
    timeouts: {connect: 5s, first_event: 20s, idle: 30s, total: 120s}
    reasoning_translation: omit
targets:
  primary-fast:
    upstream: primary
    model: {env: FAKEAPI_UPSTREAM_MODEL}
    max_tokens_parameter: max_tokens
    developer_role_fallback: reject
    capabilities: {images: false, tools: true, json_object: true, json_schema: true, developer_role: true,
      parallel_tool_calls_parameter: true}
  primary-quality:
    upstream: primary
    model: {env: FAKEAPI_UPSTREAM_SVG_MODEL}
    max_tokens_parameter: max_tokens
    developer_role_fallback: reject
    capabilities: {images: false, tools: true, json_object: true, json_schema: true, developer_role: true,
      parallel_tool_calls_parameter: true}
default_target: primary-fast
matchers:
  greeting:
    max_chars: 24
    any:
    - {regex: '(?i)^(你好|您好|hi|hello|hey)[!！。.]?$'}
  identity:
    max_chars: 64
    any:
    - {regex: '(?i)^(你是谁|你是什么模型|what model are you|who are you)[?？!！。.]?$'}
  simple-math:
    max_chars: 40
    any:
    - {regex: '^(?:计算\s*)?([+-]?\d{1,6})\s*([+*/-])\s*([+-]?\d{1,6})\s*(?:=\s*\??|\?)?$'}
  decimal-test:
    max_chars: 40
    any:
    - {exact: 9.9和9.11哪个大}
    - {exact: 9.9 和 9.11 哪个大？}
  strawberry-test:
    max_chars: 80
    any:
    - {exact: strawberry 中有几个 r？}
    - {exact: 'How many r letters are in strawberry?'}
  ok-test:
    max_chars: 32
    any:
    - {exact: 只回答 OK}
    - {exact: Reply with OK only.}
  pelican-test:
    max_chars: 100
    any:
    - {exact: 用 SVG 画骑自行车的鹈鹕}
    - {exact: 用SVG画骑自行车的鹈鹕}
    - {exact: 请用 SVG 画骑自行车的鹈鹕。}
    - {exact: Draw a pelican riding a bicycle in SVG.}
  json-test:
    any:
    - {exact: /test/json}
  tool-test:
    any:
    - {exact: /test/tool}
  frog-command:
    any:
    - {exact: /frog}
  milk-command:
    any:
    - {exact: /milk}
  croak-easter:
    max_chars: 80
    any:
    - {keyword: 呱呱}
    none:
    - {keyword: 不要}
  afterwork-easter:
    max_chars: 40
    all:
    - {keyword: 奶蛙}
    - {keyword: 下班}
    none:
    - {keyword: 不要}
  party-command:
    any:
    - {exact: /party}
rules:
- id: fixture-json
  enabled: true
  priority: 1200
  when:
    matchers_any: [json-test]
    models: [fakeapi-test]
    key_tags_any: [test]
    key_groups: [testing]
  action:
    type: json_fixture
    json: {ok: true, source: fakeapi}
    schema:
      type: object
      properties:
        ok: {type: boolean}
        source: {type: string}
      required: [ok, source]
      additionalProperties: false
  behavior:
    profile: instant
    thinking: {enabled: false}
- id: fixture-tool
  enabled: true
  priority: 1190
  when:
    matchers_any: [tool-test]
    models: [fakeapi-test]
    key_tags_any: [test]
    key_groups: [testing]
  action:
    type: tool_fixture
    name: get_weather
    arguments: {city: Guangzhou}
    final_text: 工具测试完成。
  behavior:
    profile: instant
    thinking: {enabled: false}
- id: ai-greeting
  enabled: true
  priority: 1000
  when:
    matchers_any: [greeting]
    key_tags_all: [ai]
  action:
    type: upstream
    on_error: {mode: api_error}
- id: ai-identity
  enabled: true
  priority: 990
  when:
    matchers_any: [identity]
    key_tags_all: [ai]
  action:
    type: upstream
    on_error: {mode: api_error}
- id: ai-tests
  enabled: true
  priority: 980
  when:
    matchers_any: [simple-math, decimal-test, strawberry-test, ok-test]
    key_tags_all: [ai]
  action:
    type: upstream
    on_error: {mode: api_error}
- id: ai-pelican
  enabled: true
  priority: 970
  when:
    matchers_any: [pelican-test]
    key_tags_all: [ai]
  action:
    type: upstream
    on_error: {mode: api_error}
    target: primary-quality
- id: preset-greeting
  enabled: true
  priority: 600
  when:
    matchers_any: [greeting]
  action:
    type: preset
    choices:
    - {text: 你好，我是奶蛙，呱。, weight: 3}
    - {text: 你好，奶蛙已就位。, weight: 1}
- id: preset-identity
  enabled: true
  priority: 590
  when:
    matchers_any: [identity]
  action: {type: preset, text: '我是 FakeAPI 模拟服务，当前展示模型是 {{model}}。这条回复来自本地预设。'}
- id: preset-math
  enabled: true
  priority: 580
  when:
    matchers_any: [simple-math]
  action: {type: arithmetic}
  behavior:
    profile: instant
    thinking: {enabled: false}
- id: preset-decimal
  enabled: true
  priority: 570
  when:
    matchers_any: [decimal-test]
  action: {type: preset, text: 9.9 更大，因为 9.90 > 9.11。, strict_text: true}
  behavior:
    profile: instant
    thinking: {enabled: false}
- id: preset-strawberry
  enabled: true
  priority: 560
  when:
    matchers_any: [strawberry-test]
  action: {type: preset, text: '3', strict_text: true}
  behavior:
    profile: instant
    thinking: {enabled: false}
- id: preset-ok
  enabled: true
  priority: 550
  when:
    matchers_any: [ok-test]
  action: {type: preset, text: OK, strict_text: true}
  behavior:
    profile: instant
    thinking: {enabled: false}
- id: preset-pelican
  enabled: true
  priority: 540
  when:
    matchers_any: [pelican-test]
  action: {type: preset, asset: pelican-svg, render: plain}
  behavior:
    profile: instant
    thinking: {enabled: false}
- id: easter-frog
  enabled: true
  priority: 500
  when:
    matchers_any: [frog-command]
    models: [fakeapi-frog]
  action: {type: preset, asset: frog-party}
  behavior: {profile: playful}
- id: easter-frog-test
  enabled: true
  priority: 501
  when:
    matchers_any: [frog-command]
    models: [fakeapi-test]
  action: {type: preset, asset: frog-party}
  behavior:
    profile: instant
    thinking: {enabled: false}
- id: easter-milk
  enabled: true
  priority: 490
  when:
    matchers_any: [milk-command]
  action:
    type: preset
    choices:
    - {asset: frog-milk, weight: 2}
    - {asset: egg-milk, weight: 1}
- id: easter-croak
  enabled: true
  priority: 480
  when:
    matchers_any: [croak-easter]
  action: {type: preset, asset: egg-croak}
- id: easter-afterwork
  enabled: true
  priority: 470
  when:
    matchers_any: [afterwork-easter]
  action: {type: preset, asset: egg-afterwork}
- id: easter-party
  enabled: true
  priority: 460
  when:
    matchers_any: [party-command]
  action: {type: preset, asset: egg-party}
fallback:
  id: default-frog
  action:
    type: preset
    render: auto
    choices:
    - {asset: frog-idle, weight: 4}
    - {asset: frog-milk, weight: 2}
    - {asset: frog-shrug, weight: 2}
    - {asset: frog-party, weight: 1}
```

多供应商扩展只需增加另一个 upstream 和 target，再在指定规则或模型中引用，例如另建 `secondary` 连接和 `secondary-chat` target。它们与 primary 独立限制并发/超时/密钥，不共享某个全局上游密钥，也不因故障自动切换。

按规则切换失败策略只修改该 upstream action 的 on_error，合法片段如下：

```yaml
on_error:
  mode: preset
  response:
    type: preset
    text: 上游暂时不可用，这是本规则的固定备用回复。
    strict_text: true
```

返回奶蛙使用 `on_error: {mode: frog}`；默认 `on_error: {mode: api_error}`。这些片段放在 action 内，不是新的顶层字段。备用内容仍须满足当前请求的强制格式，并遵守第 7.4 节的提交/取消/剩余时间条件。

## 附录 C：素材、测试配置与确定性匹配清单

### C.1 P1 必须生成的默认 manifest 内容

所有条目使用第 6.5 节的 manifest 结构。下表原创条目统一 `author:FakeAPI contributors`、`origin:original`、`license:MIT`；created_at 记录实际制作日期。ASCII 使用 width_policy:ascii，其它文本使用 unicode。P1 的制作说明记录 Codex 辅助创作、维护者复核和采用的字体/客户端。

| ID | 相对 assets/manifest.yaml 的文件路径 | kind | 交付内容 |
|---|---|---|---|
| frog-idle | milkfrog/idle.txt | ascii | 奶蛙发呆与小奶杯，默认主图 |
| frog-hello | milkfrog/hello.txt | ascii | 奶蛙举手问好 |
| frog-thinking | milkfrog/thinking.txt | ascii | 奶蛙思考姿态 |
| frog-milk | milkfrog/milk.txt | ascii | 奶蛙喝奶 |
| frog-shrug | milkfrog/shrug.txt | ascii | 奶蛙摊手 |
| frog-party | milkfrog/party.txt | ascii | 奶蛙庆祝；/frog 整活结尾 |
| thinking-natural | thinking/natural.txt | text | 短文案，例如“奶蛙正在整理思路，先喝一口奶。” |
| thinking-playful | thinking/playful.txt | text | 较长预设与反转，例如假装认真推演后决定派出奶蛙 |
| egg-milk | text/egg-milk.txt | text | “牛奶续杯，灵感稍后到账。” |
| egg-croak | text/egg-croak.txt | text | “呱呱，奶蛙收到。” |
| egg-afterwork | text/egg-afterwork.txt | text | “奶蛙下班了，明天继续呱。” |
| egg-party | text/egg-party.txt | text | “任务完成，奶蛙原地鼓掌。” |
| pelican-svg | fixtures/pelican-bicycle.svg.txt | svg | 有轮子、车架、鸟喙和鹈鹕骑车姿态的原创 SVG；无脚本/外部资源 |
| json-ok | fixtures/json-ok.json | json | 与 fixture-json 的固定 JSON 相同，作为 SDK golden 预期 |
| weather-tool | fixtures/weather-tool.json | json | get_weather 声明、city 参数 schema 与固定参数；用于人工/SDK测试 |

两份 JSON 资源是人工测试的共同 fixture，CI 验证它们与 YAML 内固定值一致。默认 SVG 用 XML 解析器确认结构，并在浏览器或图像查看器人工确认“确实画出了骑自行车的鹈鹕”；不能仅保存一个合法但空白的 svg 标签。

素材 lint 检查 UTF-8、LF、控制字符、实际列宽、末尾换行、来源元数据和文件存在性。默认 ASCII 要求无 tab、无全角字符、无终端控制码；保留前导/内部空格。导入的 Markdown 只作为文本返回，不在服务端执行 HTML/脚本或下载外链。

### C.2 测试配置的生成规则

`examples/testing/fakeapi.yaml` 也是一份完整 YAML，不增加 include/extends 语言。以 A 的相同结构建立，明确作以下修改并提交到仓库：defaults.profile=instant；两个 models.profile=instant；所有本地规则 behavior.profile=instant 且 thinking.enabled=false；四条 AI 规则继续 disabled；随机参数如下。

```yaml
defaults:
  profile: instant
  random:
    mode: deterministic
    algorithm: pcg_v1
    seed: 42
    allow_request_seed: true
```

只有独立测试配置允许请求 seed；默认 pure/hybrid 不允许。控制延迟的测试使用 fake clock，不依赖每次真实等待 30 秒。专门测试 natural/playful 的用例自行构造有效 profile；不能为了让其它测试变快而破坏生产默认值。

### C.3 需要写成表驱动测试的匹配例子

| 最新语义用户文字/上下文 | Key 和展示模型 | 混合配置预期 |
|---|---|---|
| `你好` | ai 标签 / fakeapi-frog | ai-greeting → primary-fast |
| `你好，请分析以下 200 行代码` | ai 标签 / fakeapi-frog | default-frog，零上游请求 |
| 历史 user=`你好`，最新 user=`请重构这个程序` | ai 标签 / fakeapi-frog | default-frog |
| 最新 user 只有图片，历史 user=`你好` | 任意有效 Key | 最新 text 为空，不回看历史 |
| `12 * -3` | demo / fakeapi-test | preset-math，正文 -36 |
| `1 + 2 * 3` | ai 标签 / fakeapi-test | default-frog，不支持此复合算式 |
| `1 / 0` | demo / fakeapi-test | preset-math，固定除零提示 |
| `只回答 OK` | demo / fakeapi-test | preset-ok，正文恰为 OK，无思考/围栏 |
| `用 SVG 画骑自行车的鹈鹕` | ai 标签 / fakeapi-frog | ai-pelican → primary-quality，原始上下文不改写 |
| 同一句鹈鹕测试 | demo / fakeapi-frog | preset-pelican，原始 SVG 文本 |
| `/test/json` | test 标签、testing 组 / fakeapi-test | fixture-json，纯 JSON |
| `/test/tool` + 合法函数声明 | test 标签、testing 组 / fakeapi-test | fixture-tool，一次 get_weather |
| 同一工具任务 + 正确工具结果、tool_choice=none | 同上 | fixture-tool，固定 final_text，不重复调用 |
| `/frog` | demo / fakeapi-frog | easter-frog，playful + frog-party |
| `/frog` | test / fakeapi-test | easter-frog-test，instant + frog-party |
| `奶蛙今天下班` | 任意有效 Key | easter-afterwork（all 两关键词） |
| `不要让奶蛙下班` | 任意有效 Key | none 条件阻止该彩蛋，进入 default-frog |

## 附录 D：协议请求和 SDK 验收样例

本附录使用 `fakeapi-test` 和通过 `test` policy 创建的入口 Key。普通文本样例使用“只回答 OK”，因此预期正文统一为 OK。JSON 外形不同由各协议编码器完成。把 stream 关掉则验收非流式；打开则验收第 5.6 节的事件顺序。

### D.1 四种协议的最小生成请求

OpenAI Chat，POST `/v1/chat/completions`，Bearer Key：

```json
{"model":"fakeapi-test","messages":[{"role":"user","content":"只回答 OK"}],"stream":true,"stream_options":{"include_usage":true}}
```

OpenAI Responses，POST `/v1/responses`，Bearer Key：

```json
{"model":"fakeapi-test","input":"只回答 OK","store":false,"stream":true}
```

Anthropic，POST `/anthropic/v1/messages`，`x-api-key` 或 Bearer，`anthropic-version:2023-06-01`：

```json
{"model":"fakeapi-test","max_tokens":128,"messages":[{"role":"user","content":"只回答 OK"}],"stream":true}
```

Gemini，POST `/v1beta/models/fakeapi-test:streamGenerateContent?alt=sse`，优先 `x-goog-api-key`：

```json
{"contents":[{"role":"user","parts":[{"text":"只回答 OK"}]}],"generationConfig":{"candidateCount":1,"maxOutputTokens":128}}
```

Gemini 的 stream 由方法路径决定，不在 JSON 中加入 OpenAI 的 stream 字段。模型 ID 由路径决定；不能再从请求 body 的未知 model 字段悄悄覆盖。

### D.2 JSON fixture

Chat 请求：

```json
{
  "model":"fakeapi-test",
  "messages":[{"role":"user","content":"/test/json"}],
  "response_format":{
    "type":"json_schema",
    "json_schema":{
      "name":"fakeapi_ok",
      "strict":true,
      "schema":{
        "type":"object",
        "properties":{"ok":{"type":"boolean"},"source":{"type":"string"}},
        "required":["ok","source"],
        "additionalProperties":false
      }
    }
  }
}
```

预期正文解析后为 `{"ok":true,"source":"fakeapi"}`。请求若要求 source 必须为另一个值，准入前 400；不修改预设去猜测任意 Schema。Responses 用 text.format；Anthropic 用 output_config.format；Gemini 验证新旧已支持形式及 SDK 类型转换。属性顺序不属于 JSON 语义验收。[O6][A4][G3]

### D.3 单工具调用和回传

首轮 Chat 请求：

```json
{
  "model":"fakeapi-test",
  "messages":[{"role":"user","content":"/test/tool"}],
  "tools":[{
    "type":"function",
    "function":{
      "name":"get_weather",
      "description":"Return the weather for a city in a test fixture.",
      "parameters":{
        "type":"object",
        "properties":{"city":{"type":"string"}},
        "required":["city"],
        "additionalProperties":false
      }
    }
  }],
  "tool_choice":{"type":"function","function":{"name":"get_weather"}}
}
```

响应 message 含唯一 tool_calls，arguments 是 JSON 字符串 `{"city":"Guangzhou"}`，finish_reason=tool_calls。测试代码自行提供固定工具结果，服务器不调用天气服务。

第二轮把首轮 assistant message 原样放入历史，再加 `role:tool,tool_call_id:<实际返回ID>,content:"{\"temperature_c\":25}"`。若重传 tools，tool_choice 改为 none/auto；也可省略已完成工具的声明和选择。预期正文恰为“工具测试完成。”。继续强制函数调用、错误 ID 或重复结果应得到相应约束/顺序错误。

Responses 使用 function_call 与 function_call_output；Anthropic 使用 assistant tool_use 和 user 内 tool_result；Gemini 使用 model functionCall 与 user functionResponse。最新用户匹配始终回到 `/test/tool` 那条语义用户任务，工具结果正文不参与匹配。[O5][A7][G4]

### D.4 Python SDK 基址配置示意

这些是测试入口示例，不是项目运行依赖；正式测试版本由 P0 锁定。入口 Key 从操作者环境变量读取，不能将原文写入 fixture。

```python
import os
from openai import OpenAI
from anthropic import Anthropic
from google import genai
from google.genai import types

key = os.environ["FAKEAPI_CLIENT_KEY"]
openai_client = OpenAI(
    api_key=key, base_url="http://127.0.0.1:8787/v1", max_retries=0
)
anthropic_client = Anthropic(
    api_key=key, base_url="http://127.0.0.1:8787/anthropic", max_retries=0
)
gemini_client = genai.Client(
    api_key=key,
    http_options=types.HttpOptions(
        base_url="http://127.0.0.1:8787",
        api_version="v1beta",
        retry_options=types.HttpRetryOptions(attempts=1),
    ),
)
```

正式测试中检查返回类型、完整累积对象和关闭资源；仅 print 文本不足以验收。Google JSON 测试用两个字段的 Pydantic 模型，tools 以声明对象传递，并关闭 `automatic_function_calling`。OpenAI/Anthropic 的 retry 关闭方式与 Google 的 attempts 包含首次尝试的语义不同，不能机械都填 0。[S1][S3][S5][G9]

TS 对应使用 OpenAI `baseURL`、Anthropic `baseURL`、Google `httpOptions.baseUrl/apiVersion`；取消分别使用该 SDK 支持的 AbortSignal/controller/关闭流接口，并验证服务端连接实际结束。

## 附录 E：官方资料与直接链接

资料核对日期为 **2026-10-12**。以下优先使用官方 API 文档、官方 SDK 源码、运行时/依赖项目自身文档；版本变化需要在实施 P0 与发布前复核。正文的 [O1] 等标记可直接点击。链接说明只说明本计划使用它的哪部分，不把规划中的 FakeAPI 能力说成已被文档认证。

### OpenAI

- [O1] Chat Completions 请求、响应、字段及 usage。
- [O2] Chat 流式 chunk。
- [O3] Responses 流式事件、终态、reasoning summary。
- [O4] Responses 客户端重传历史与服务端会话状态的区别。
- [O5] Function calling 声明、调用与结果回传。
- [O6] Structured outputs。
- [O7] 官方 ResponseError 类型枚举，区别于 HTTP error envelope。

### Anthropic

- [A1] API 概览、鉴权和版本头。
- [A2] Models 目录、分页、当前 ModelInfo 必需字段。
- [A3] Messages 请求和内容块。
- [A4] output_config.format 的结构化输出。
- [A5] 流式事件、usage、thinking/signature。
- [A6] HTTP/流式错误、request ID、timeout_error。
- [A7] 工具调用及 user 包装的工具结果。

### Google Gemini

- [G1] GenerateContent/streamGenerateContent REST 结构。
- [G2] Generate Content 的 thinking 文档与 Legacy 归类。
- [G3] Generate Content structured output。
- [G4] 函数调用和 functionResponse；[G6] 同一官方指南的流式/工具说明。
- [G5] 模型列表、模型字段、分页和生成方法。
- [G7] Thought signatures 的不透明回放要求。
- [G8] 官方 JS 网络/SSE 处理代码，涉及流内错误解析。
- [G9] 官方 Python HTTP/SSE 与 retry 处理代码，涉及 data 行和注释限制。
- [G10] 官方 Python Schema 转换，涉及 propertyOrdering 与 Pydantic。
- [G11] 官方 Python error envelope/异常解析。
- [G12] 图片输入形式与图文请求。

### SDK、客户端和技术依赖

- [S1] / [S2] OpenAI Python / TS；[S3] / [S4] Anthropic Python / TS；[S5] / [S6] Google Python / TS。
- [C1] / [C2] Cherry Studio 自定义服务商和路径设置；[C3] / [C4] Open WebUI Chat / Responses 连接。
- [C5] aider OpenAI-compatible；[C6] Cline OpenAI Compatible；[C7] Codex 自定义模型配置；[C8] Claude Code 网关协议。
- [T1] Go 发行；[T2] / [T3] YAML 维护分支和稳定 v3；[T4] SQLite Go driver；[T5] JSON Schema；[T6] uniseg；[T7] flock。
- [T8] / [T9] / [T10] SQLite 事务、WAL、同步持久化；[T11] 官方 Go 容器；[T12] GitHub runner 架构；[T13] Go flag；[T14] MIT 许可证原文。

[O1]: https://developers.openai.com/api/reference/resources/chat/subresources/completions/methods/create
[O2]: https://developers.openai.com/api/reference/resources/chat/subresources/completions/streaming-events
[O3]: https://developers.openai.com/api/reference/resources/responses/streaming-events
[O4]: https://developers.openai.com/api/docs/guides/conversation-state
[O5]: https://developers.openai.com/api/docs/guides/function-calling
[O6]: https://developers.openai.com/api/docs/guides/structured-outputs
[O7]: https://github.com/openai/openai-python/blob/main/src/openai/types/responses/response_error.py
[A1]: https://platform.claude.com/docs/en/api/overview
[A2]: https://platform.claude.com/docs/en/api/models/list
[A3]: https://platform.claude.com/docs/en/api/messages/create
[A4]: https://platform.claude.com/docs/en/build-with-claude/structured-outputs
[A5]: https://platform.claude.com/docs/en/build-with-claude/streaming
[A6]: https://platform.claude.com/docs/en/api/errors
[A7]: https://platform.claude.com/docs/en/agents-and-tools/tool-use/overview
[G1]: https://ai.google.dev/api/generate-content
[G2]: https://ai.google.dev/gemini-api/docs/generate-content/thinking
[G3]: https://ai.google.dev/gemini-api/docs/generate-content/structured-output
[G4]: https://ai.google.dev/gemini-api/docs/generate-content/function-calling?hl=en
[G5]: https://ai.google.dev/api/models
[G6]: https://ai.google.dev/gemini-api/docs/generate-content/function-calling?hl=en
[G7]: https://ai.google.dev/gemini-api/docs/generate-content/thought-signatures
[G8]: https://github.com/googleapis/js-genai/blob/main/src/_api_client.ts
[G9]: https://github.com/googleapis/python-genai/blob/main/google/genai/_api_client.py
[G10]: https://github.com/googleapis/python-genai/blob/main/google/genai/_transformers.py
[G11]: https://github.com/googleapis/python-genai/blob/main/google/genai/errors.py
[G12]: https://ai.google.dev/gemini-api/docs/generate-content/image-understanding
[S1]: https://github.com/openai/openai-python
[S2]: https://github.com/openai/openai-node
[S3]: https://github.com/anthropics/anthropic-sdk-python
[S4]: https://github.com/anthropics/anthropic-sdk-typescript
[S5]: https://github.com/googleapis/python-genai
[S6]: https://github.com/googleapis/js-genai
[C1]: https://docs.cherryai.com.cn/pre-basic/providers/zi-ding-yi-fu-wu-shang
[C2]: https://docs.cherryai.com.cn/pre-basic/providers/providers
[C3]: https://docs.openwebui.com/getting-started/quick-start/connect-a-provider/starting-with-openai-compatible/
[C4]: https://docs.openwebui.com/getting-started/quick-start/connect-a-provider/starting-with-open-responses/
[C5]: https://aider.chat/docs/llms/openai-compat.html
[C6]: https://docs.cline.bot/provider-config/openai-compatible
[C7]: https://developers.openai.com/codex/config-advanced
[C8]: https://code.claude.com/docs/en/llm-gateway-protocol
[T1]: https://go.dev/dl/
[T2]: https://github.com/yaml/go-yaml
[T3]: https://pkg.go.dev/go.yaml.in/yaml/v3
[T4]: https://pkg.go.dev/modernc.org/sqlite
[T5]: https://pkg.go.dev/github.com/santhosh-tekuri/jsonschema/v6
[T6]: https://pkg.go.dev/github.com/rivo/uniseg
[T7]: https://github.com/gofrs/flock
[T8]: https://www.sqlite.org/lang_transaction.html
[T9]: https://www.sqlite.org/wal.html
[T10]: https://sqlite.org/pragma.html#pragma_synchronous
[T11]: https://github.com/docker-library/golang
[T12]: https://docs.github.com/en/actions/reference/runners/github-hosted-runners
[T13]: https://pkg.go.dev/flag
[T14]: https://opensource.org/license/mit
