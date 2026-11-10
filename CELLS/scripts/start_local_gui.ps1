param(
    [string]$CELLSRoot = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path,
    [switch]$NoOpen,
    [switch]$NoStayOpen,
    [switch]$AllowParallel
)

$ErrorActionPreference = "Stop"

$CELLSRoot = (Resolve-Path $CELLSRoot).Path

. (Join-Path $CELLSRoot "scripts\lib\CELLS-endpoints.ps1")
$LocalRoot = Join-Path $CELLSRoot "source\.cache\local-validator"
$UrlFile = Join-Path $LocalRoot "local-gui-persist.url"
$OutLog = Join-Path $LocalRoot "local-gui-persist.out.log"
$ErrLog = Join-Path $LocalRoot "local-gui-persist.err.log"

New-Item -ItemType Directory -Force -Path $LocalRoot | Out-Null

$candidates = @(
    "CELLS-local-gui-home-server.exe",
    "CELLS-local-gui-hive-v2.exe",
    "CELLS-local-gui-hive.exe",
    "CELLS-local-gui-persist.exe",
    "CELLS-local-gui-next.exe",
    "CELLS-local-gui-sqlite.exe",
    "CELLS-local-gui.exe"
)

$ExePath = $null
foreach ($candidate in $candidates) {
    $path = Join-Path $LocalRoot $candidate
    if (Test-Path -LiteralPath $path) {
        $ExePath = $path
        break
    }
}
if ($null -eq $ExePath) {
    throw "Missing local GUI executable in $LocalRoot"
}

$running = Get-Process -ErrorAction SilentlyContinue | Where-Object {
    $_.ProcessName -like "CELLS-local-gui*"
}
if ($running -and -not $AllowParallel) {
    Write-Host "CELLS local GUI is already running."
    exit 0
}

$env:CELLS_LOCAL_GUI_URL_FILE = $UrlFile
if ($NoStayOpen) {
    Remove-Item Env:\CELLS_LOCAL_GUI_STAY_OPEN -ErrorAction SilentlyContinue
} else {
    $env:CELLS_LOCAL_GUI_STAY_OPEN = "1"
}
if ($NoOpen) {
    $env:CELLS_LOCAL_GUI_NO_OPEN = "1"
} else {
    Remove-Item Env:\CELLS_LOCAL_GUI_NO_OPEN -ErrorAction SilentlyContinue
}

$env:HTTP_PROXY = ""
$env:HTTPS_PROXY = ""
$env:ALL_PROXY = ""
$env:NO_PROXY = Get-CELLSNoProxyList -EndpointValues @(
    "http://127.0.0.1:8080",
    (Get-CELLSEndpointValue -Name "core_api_base" -EnvVar "CELLS_CHAIN_SYNC_URLS"),
    (Get-CELLSEndpointValue -Name "home_gateway_relay" -EnvVar "CELLS_HOME_GATEWAY_RELAY")
)

$process = Start-Process `
    -FilePath $ExePath `
    -WorkingDirectory $CELLSRoot `
    -WindowStyle Hidden `
    -RedirectStandardOutput $OutLog `
    -RedirectStandardError $ErrLog `
    -PassThru

Write-Host "CELLS local GUI started pid=$($process.Id)"
