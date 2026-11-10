param()

$ErrorActionPreference = "Stop"

$startup = [Environment]::GetFolderPath("Startup")
if ([string]::IsNullOrWhiteSpace($startup)) {
    throw "Could not locate the current user's Startup folder"
}

$launcher = Join-Path $startup "CELLS-Tray-Monitor.vbs"
Remove-Item -LiteralPath $launcher -Force -ErrorAction SilentlyContinue
Write-Host "Removed CELLS Tray Monitor Startup launcher if it existed: $launcher"
