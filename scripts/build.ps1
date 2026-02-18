Param(
  [string]$OutputDir = ".\dist"
)

$ErrorActionPreference = "Stop"
$env:GOCACHE = Join-Path (Get-Location) ".gocache"

if (!(Test-Path $OutputDir)) {
  New-Item -ItemType Directory -Path $OutputDir | Out-Null
}

Write-Host "[build] building bot.exe..."
go build -o (Join-Path $OutputDir "bot.exe") .\cmd\bot
if ($LASTEXITCODE -ne 0) {
  throw "[build] build bot.exe failed"
}

Write-Host "[build] building bot_config.exe..."
go build -o (Join-Path $OutputDir "bot_config.exe") .\cmd\configui
if ($LASTEXITCODE -ne 0) {
  throw "[build] build bot_config.exe failed"
}

Write-Host "[build] done:"
Get-ChildItem $OutputDir | Where-Object { $_.Name -in @("bot.exe", "bot_config.exe") } | Select-Object Name, Length, LastWriteTime
