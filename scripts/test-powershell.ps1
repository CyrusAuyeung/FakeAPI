$ErrorActionPreference = 'Stop'
if (-not $env:FAKEAPI_POWERSHELL_TEST_KEY -or -not $env:FAKEAPI_POWERSHELL_TEST_URL) {
    throw 'Missing isolated test environment'
}
if (-not $env:FAKEAPI_POWERSHELL_TEST_URL.StartsWith('http://127.0.0.1:')) {
    throw 'PowerShell acceptance is loopback only'
}
function Ask-FakeAPI {
    param([string]$Message)
    $body = @{
        model = 'fakeapi-test'
        messages = @(@{ role = 'user'; content = $Message })
    } | ConvertTo-Json -Depth 8
    $reply = Invoke-RestMethod `
        -Uri ($env:FAKEAPI_POWERSHELL_TEST_URL + '/v1/chat/completions') `
        -Method Post -Headers @{ Authorization = "Bearer $env:FAKEAPI_POWERSHELL_TEST_KEY" } `
        -ContentType 'application/json' `
        -Body ([System.Text.Encoding]::UTF8.GetBytes($body))
    $reply.choices[0].message.content
}
if ((Ask-FakeAPI '1+1') -ne '2') { throw 'Arithmetic result mismatch' }
# Source remains ASCII for Windows PowerShell 5.1's BOM-less file handling.
$hello = [string][char]0x4f60 + [string][char]0x597d
if (-not (Ask-FakeAPI $hello).StartsWith($hello)) { throw 'Chinese UTF-8 round trip failed' }
if (-not (Ask-FakeAPI '/frog').Contains('```')) { throw 'ASCII response missing' }
if (-not (Ask-FakeAPI 'Draw a pelican riding a bicycle in SVG.').Contains('<svg')) { throw 'SVG fixture missing' }
Write-Output 'PASS Windows PowerShell JSON, Chinese UTF-8, ASCII and SVG'
