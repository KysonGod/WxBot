Param(
  [string]$OutputDir = ".\dist"
)

$ErrorActionPreference = "Stop"
$env:GOCACHE = Join-Path (Get-Location) ".gocache"

if (!(Test-Path $OutputDir)) {
  New-Item -ItemType Directory -Path $OutputDir | Out-Null
}

Write-Host "[build] building WxBot.exe..."
go build -o (Join-Path $OutputDir "WxBot.exe") .\cmd\bot
if ($LASTEXITCODE -ne 0) {
  throw "[build] build WxBot.exe failed"
}

# cleanup legacy/extra binaries to enforce single entrypoint
@("bot.exe", "bot_config.exe", "configui.exe") | ForEach-Object {
  $legacy = Join-Path $OutputDir $_
  if (Test-Path $legacy) {
    Remove-Item $legacy -Force
  }
}

Write-Host "[build] done:"
Get-ChildItem $OutputDir | Where-Object { $_.Name -in @("WxBot.exe") } | Select-Object Name, Length, LastWriteTime
