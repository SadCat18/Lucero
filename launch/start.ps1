#requires -Version 7
# 启动：pwsh -NoProfile -File launch/start.ps1（PowerShell 7；系统自带的 5.1 不要用）
# 一键起：Core + 面板（浏览器）。停止用 stop.ps1。
# 约定：不新增依赖、不改任何业务代码；端口由 Core 随机分配并写进 core.endpoint（REQ-L-1）。
$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent (Split-Path -Parent $MyInvocation.MyCommand.Path)
$run  = Join-Path $root '.lucero-run'
New-Item -ItemType Directory -Force -Path $run | Out-Null
$go = 'D:\DevTools\go\bin\go.exe'
$node = 'D:\DevTools\node-v24.19.0-win-x64\node.exe'
if (-not (Test-Path $go))   { throw "找不到 Go：$go" }
if (-not (Test-Path $node)) { throw "找不到 Node：$node" }

Write-Host '[1/4] 编译 Core 与假适配器' -ForegroundColor Cyan
Push-Location (Join-Path $root 'core')
& $go build -o (Join-Path $run 'core.exe') .\cmd\core
& $go build -o (Join-Path $run 'fake-adapter.exe') .\cmd\fake-adapter
Pop-Location
if ($LASTEXITCODE -ne 0) { throw '编译失败，见上面输出' }

Write-Host '[2/4] 起 Core（数据目录 .lucero-run\lucero-data）' -ForegroundColor Cyan
$env:LUCERO_DATA_DIR = Join-Path $run 'lucero-data'
$core = Start-Process -FilePath (Join-Path $run 'core.exe') `
  -ArgumentList @('-migrations', (Join-Path $root 'core\migrations'), '-adapter', "fake=$(Join-Path $run 'fake-adapter.exe')") `
  -WorkingDirectory (Join-Path $root 'core') -PassThru -WindowStyle Minimized
$core.Id | Out-File -Encoding ascii (Join-Path $run 'core.pid')

$ep = Join-Path $env:LUCERO_DATA_DIR 'core.endpoint'
for ($i = 0; $i -lt 40; $i++) { if (Test-Path $ep) { break }; Start-Sleep -Milliseconds 250 }
if (-not (Test-Path $ep)) { throw "等了 10 秒没出现 $ep —— Core 没起来，去看它的窗口输出" }
$info = Get-Content $ep -Raw | ConvertFrom-Json
Write-Host ("     Core 在 http://{0}（口令只写进本机环境变量，不落任何文件）" -f $info.host_port) -ForegroundColor Gray

Write-Host '[3/4] 起面板（Vite dev，代理自动注入口令）' -ForegroundColor Cyan
$env:LUCERO_CORE_ORIGIN = 'http://' + $info.host_port
$env:LUCERO_CORE_TOKEN  = $info.token
Push-Location (Join-Path $root 'apps\desktop')
$ui = Start-Process -FilePath cmd.exe `
  -ArgumentList @('/c', 'npm run dev -- --strictPort --port 5173 > "%s" 2>&1' -f (Join-Path $run 'ui.log')) `
  -WorkingDirectory (Join-Path $root "apps\desktop") -PassThru -WindowStyle Minimized
$ui.Id | Out-File -Encoding ascii (Join-Path $run 'ui.pid')

Start-Sleep -Seconds 3
Get-Content (Join-Path $run 'ui.log') -Tail 3 -ErrorAction SilentlyContinue
Write-Host '[4/4] 打开浏览器：http://localhost:5173' -ForegroundColor Green
Start-Process 'http://localhost:5173'
Write-Host '停止：powershell -File .\launch\stop.ps1' -ForegroundColor Yellow
