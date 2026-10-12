# Windows 本机使用，无需 AI 客户端

从 GitHub Release 下载 Windows amd64 ZIP 并解压。在解压目录打开 PowerShell。
源码工作目录使用 `D:\Projects\FakeAPI\bin\fakeapi.exe`，可以先进入 `bin`。

## 启动

```powershell
Set-Location D:\Projects\FakeAPI\bin
.\fakeapi.exe init --template preset --dir .\runtime\preset
.\fakeapi.exe config check --config .\runtime\preset\fakeapi.yaml
.\fakeapi.exe keys create --config .\runtime\preset\fakeapi.yaml --policy demo --label local --expires-in 30d
.\fakeapi.exe serve --config .\runtime\preset\fakeapi.yaml
```

下载发行包时，把第一行改为你的解压目录。保存创建命令显示的 Key。
`init` 只在首次运行时执行，已有配置时会拒绝覆盖。服务终端保持打开，Ctrl+C 停止。
默认监听 `127.0.0.1:8787`；demo Key 总额度 10,000 次、RPM 60、并发 4，AI 额度 0。

## 调用

另开一个 PowerShell 窗口，粘贴下面的代码并输入刚创建的 Key：

```powershell
$key = Read-Host '粘贴创建命令显示的 Key'
function Ask-FakeAPI {
    param([string]$Message, [string]$Model = 'fakeapi-test')
    $body = @{
        model = $Model
        messages = @(@{ role = 'user'; content = $Message })
    } | ConvertTo-Json -Depth 8
    $reply = Invoke-RestMethod `
        -Uri 'http://127.0.0.1:8787/v1/chat/completions' `
        -Method Post -Headers @{ Authorization = "Bearer $key" } `
        -ContentType 'application/json' `
        -Body ([System.Text.Encoding]::UTF8.GetBytes($body))
    $reply.choices[0].message.content
}
Ask-FakeAPI '1+1'
Ask-FakeAPI '你好'
Ask-FakeAPI '/frog'
Ask-FakeAPI '用 SVG 画骑自行车的鹈鹕'
```

依次应得到 `2`、自然问候、挥手字符画及 SVG 文本。`fakeapi-test` 为极速输出；
将模型参数换成 `fakeapi-frog` 可体验自然节奏：

```powershell
Ask-FakeAPI '随便聊聊' 'fakeapi-frog'
```

四份字符画的等宽预览见 [ascii-preview.html](ascii-preview.html)。
PowerShell 的 `Invoke-RestMethod` 在此示例中读取完整 JSON，客户端 SSE 已有独立验收。
Windows 运行验收还实际执行 PowerShell 的数学、中文 UTF-8、字符画及 SVG 调用。

## 查看额度及撤销

在解压目录执行：

```powershell
.\fakeapi.exe keys list --config .\runtime\preset\fakeapi.yaml
.\fakeapi.exe keys show --config .\runtime\preset\fakeapi.yaml --id KEY_ID
.\fakeapi.exe keys revoke --config .\runtime\preset\fakeapi.yaml --id KEY_ID
```

把 `KEY_ID` 换为创建命令显示的公开 ID。撤销立即生效，服务无需重启。
Key 原文不会出现在 list/show 中。混合模式的环境变量和 Key policy 见 README。
