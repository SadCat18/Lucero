#requires -Version 5.1
# 一键停：只关我们自己起的那两个进程，绝不按进程名广杀。
$ErrorActionPreference = 'SilentlyContinue'
$root = Split-Path -Parent (Split-Path -Parent $MyInvocation.MyCommand.Path)
$run  = Join-Path $root '.lucero-run'

foreach ($pair in @(@('ui', 'ui.pid'), @('core', 'core.pid'))) {
  $file = Join-Path $run $pair[1]
  if (Test-Path $file) {
    $id = (Get-Content $file -Raw).Trim()
    if ($id -match '^\d+$') {
      # 先给孩子进程，再关父进程；只杀这条路径下的可执行文件，避免误伤同名进程
      $proc = Get-Process -Id $id
      if ($proc) {
        $exe = $proc.Path
        if ($exe -and (Test-Path $exe) -and $exe.StartsWith($run, [StringComparison]::OrdinalIgnoreCase)) {
          & taskkill /PID $id /T /F | Out-Null
          Write-Host ("关了 {0}（pid {1}，{2}）" -f $pair[0], $id, (Split-Path -Leaf $exe))
        } else {
          Write-Host ("跳过 {0}（pid {1}）：可执行文件不在 .lucero-run 里，不敢乱杀" -f $pair[0], $id)
        }
      }
    }
    Remove-Item $file -Force
  }
}
Write-Host '完。数据留在 .lucero-run\lucero-data（要清库自己删那个目录，我不代删）。'
