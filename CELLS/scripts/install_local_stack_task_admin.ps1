param(
    [string]$CELLSRoot = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path,
    [switch]$NoPause
)

$ErrorActionPreference = "Stop"

function Test-IsAdmin {
    $identity = [Security.Principal.WindowsIdentity]::GetCurrent()
    $principal = [Security.Principal.WindowsPrincipal]::new($identity)
    return $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
}

function Quote-Arg {
    param([string]$Value)
    return '"' + ($Value -replace '"', '\"') + '"'
}

$CELLSRoot = (Resolve-Path $CELLSRoot).Path
. (Join-Path $CELLSRoot "scripts\lib\CELLS-endpoints.ps1")
$Relay = Get-CELLSEndpointValue -Name "home_gateway_relay" -EnvVar "CELLS_HOME_GATEWAY_RELAY"
$Slot = Get-CELLSEndpointValue -Name "home_gateway_slot" -EnvVar "CELLS_HOME_GATEWAY_SLOT"
$Installer = Join-Path $CELLSRoot "scripts\install_local_stack_task.ps1"
$LogPath = Join-Path $CELLSRoot "source\.cache\local-validator\local-stack-task-install.log"

if (-not (Test-IsAdmin)) {
    $args = "-NoProfile -ExecutionPolicy Bypass -NoExit -File $(Quote-Arg $PSCommandPath) -CELLSRoot $(Quote-Arg $CELLSRoot)"
    Start-Process -FilePath "powershell.exe" -Verb RunAs -ArgumentList $args
    Write-Host "Windows administrator prompt requested."
    exit 0
}

try {
    if (-not (Test-Path -LiteralPath $Installer)) {
        throw "Missing installer: $Installer"
    }
    & powershell.exe -NoProfile -ExecutionPolicy Bypass -File $Installer `
        -CELLSRoot $CELLSRoot `
        -Relay $Relay `
        -Slot $Slot `
        -Highest `
        -AtStartup `
        -AsSystem `
        -RemoveStartupFallback `
        -NoStartupFallback

    Write-Host ""
    Write-Host "CELLS elevated Scheduled Task install finished."
    Write-Host "Log: $LogPath"
    if (-not $NoPause) {
        Write-Host "Leaving this admin window open for diagnostics."
    }
} catch {
    Write-Host ""
    Write-Host "CELLS elevated Scheduled Task install failed:" -ForegroundColor Red
    Write-Host $_.Exception.Message -ForegroundColor Red
    Write-Host "Log: $LogPath"
    if (-not $NoPause) {
        Read-Host "Press Enter to close"
    }
    exit 1
}
