#requires -Version 5.1

[CmdletBinding()]
param(
    [string]$CELLSRoot = ""
)

if ([string]::IsNullOrWhiteSpace($CELLSRoot)) {
    $scriptDirectory = Split-Path -Parent $MyInvocation.MyCommand.Path
    $CELLSRoot = (Resolve-Path (Join-Path $scriptDirectory "..")).Path
}

$ErrorActionPreference = "Stop"
Set-StrictMode -Version Latest

$launcherSource = Join-Path $CELLSRoot "scripts\start_local_validator.ps1"
$endpointLibrarySource = Join-Path $CELLSRoot "scripts\lib\CELLS-endpoints.ps1"
$testRoot = Join-Path ([IO.Path]::GetTempPath()) "CELLS-bootstrap-profile-$([guid]::NewGuid().ToString('N'))"
$fixtureRoot = Join-Path $testRoot "CELLS-root"
$defaultBootstrapPeer = "/dns4/bootstrap.CELLS.test/tcp/4001/p2p/12D3KooWJ8XnsdHyr7dPXWVsLzRD3mDxFCPkGA8EvBKMYe81rLPQ"
$explicitBootstrapPeer = "/dns4/alternate.CELLS.test/tcp/4001/p2p/12D3KooWJNWNgx8SAdGqVEcsM9AapVk7byPp4BkJnBba2rqUFXEJ"

function Assert-Equal {
    param(
        [Parameter(Mandatory)]$Actual,
        [Parameter(Mandatory)]$Expected,
        [Parameter(Mandatory)][string]$Message
    )

    if ($Actual -ne $Expected) {
        throw "$Message Expected '$Expected', got '$Actual'."
    }
}

function Invoke-ProfileOnly {
    param([string]$BootstrapPeers = "")

    $arguments = @(
        "-NoProfile",
        "-File", (Join-Path $fixtureRoot "scripts\start_local_validator.ps1"),
        "-CELLSRoot", $fixtureRoot,
        "-Networked",
        "-UpdateProfileOnly"
    )
    if (-not [string]::IsNullOrWhiteSpace($BootstrapPeers)) {
        $arguments += @("-BootstrapPeers", $BootstrapPeers)
    }

    $hostExecutable = @(
        (Join-Path $PSHOME "pwsh.exe"),
        (Join-Path $PSHOME "powershell.exe")
    ) | Where-Object { Test-Path -LiteralPath $_ -PathType Leaf } | Select-Object -First 1
    if (-not $hostExecutable) {
        throw "Could not find a PowerShell host executable under $PSHOME."
    }
    $profileOutput = @(& $hostExecutable @arguments 2>&1)
    if ($LASTEXITCODE -ne 0) {
        throw "Profile-only launcher fixture failed with exit code $LASTEXITCODE. $($profileOutput | Out-String)"
    }
}

try {
    New-Item -ItemType Directory -Force -Path `
        (Join-Path $fixtureRoot "scripts\lib"), `
        (Join-Path $fixtureRoot "config"), `
        (Join-Path $fixtureRoot "source\.cache\local-validator") | Out-Null
    Copy-Item -LiteralPath $launcherSource -Destination (Join-Path $fixtureRoot "scripts\start_local_validator.ps1") -Force
    Copy-Item -LiteralPath $endpointLibrarySource -Destination (Join-Path $fixtureRoot "scripts\lib\CELLS-endpoints.ps1") -Force

    [IO.File]::WriteAllText(
        (Join-Path $fixtureRoot "config\public-endpoints.json"),
        (@{
            core_api_base = "https://api.CELLS.test/api/v1"
            reference_bootstrap_peer = $defaultBootstrapPeer
        } | ConvertTo-Json),
        [Text.UTF8Encoding]::new($false)
    )

    $binaryPath = Join-Path $fixtureRoot "source\.cache\local-validator\fixture.exe"
    [IO.File]::WriteAllText($binaryPath, "fixture", [Text.UTF8Encoding]::new($false))
    $activeState = [ordered]@{
        schema = "CELLS.validator-active.v1"
        binary = "fixture.exe"
        sha256 = (Get-FileHash -LiteralPath $binaryPath -Algorithm SHA256).Hash.ToLowerInvariant()
    }
    [IO.File]::WriteAllText(
        (Join-Path $fixtureRoot "source\.cache\local-validator\validator-active.json"),
        ($activeState | ConvertTo-Json),
        [Text.UTF8Encoding]::new($false)
    )

    Invoke-ProfileOnly
    $defaultMode = Get-Content -Raw -LiteralPath (Join-Path $fixtureRoot "source\.cache\local-validator\validator-mode.json") | ConvertFrom-Json
    Assert-Equal -Actual $defaultMode.bootstrapPeers -Expected $defaultBootstrapPeer `
        -Message "The persisted network profile must retain the default bootstrap peer used at runtime."

    Invoke-ProfileOnly -BootstrapPeers $explicitBootstrapPeer
    $explicitMode = Get-Content -Raw -LiteralPath (Join-Path $fixtureRoot "source\.cache\local-validator\validator-mode.json") | ConvertFrom-Json
    Assert-Equal -Actual $explicitMode.bootstrapPeers -Expected $explicitBootstrapPeer `
        -Message "An explicitly supplied bootstrap peer must override the default in the persisted profile."

    [pscustomobject]@{
        schema = "CELLS.local-validator-bootstrap-profile-test.v1"
        success = $true
        default_bootstrap_persisted = $true
        explicit_bootstrap_preserved = $true
    } | ConvertTo-Json -Compress
} finally {
    Remove-Item -LiteralPath $testRoot -Recurse -Force -ErrorAction SilentlyContinue
}