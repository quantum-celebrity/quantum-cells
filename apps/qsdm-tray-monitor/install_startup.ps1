param(
    [string]$ExePath = (Join-Path $PSScriptRoot "dist\CELLS-tray-monitor.exe"),
    [string]$CELLSRoot = (Resolve-Path (Join-Path $PSScriptRoot "..\..\CELLS")).Path
)

$ErrorActionPreference = "Stop"

$ExePath = (Resolve-Path $ExePath).Path
$CELLSRoot = (Resolve-Path $CELLSRoot).Path
$startup = [Environment]::GetFolderPath("Startup")
if ([string]::IsNullOrWhiteSpace($startup)) {
    throw "Could not locate the current user's Startup folder"
}

$launcher = Join-Path $startup "CELLS-Tray-Monitor.vbs"
$command = "`"$ExePath`" --root `"$CELLSRoot`""
$vbsCommand = $command.Replace('"', '""')
Set-Content -LiteralPath $launcher -Encoding ASCII -Value @"
Set shell = CreateObject("WScript.Shell")
shell.Run "$vbsCommand", 0, False
"@

Write-Host "Installed CELLS Tray Monitor Startup launcher: $launcher"
