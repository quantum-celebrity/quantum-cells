param(
    [Parameter(Mandatory = $true)]
    [string]$ArtifactRoot,
    [string]$CELLSRoot = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path,
    [string]$OutputPath = ""
)

$ErrorActionPreference = "Stop"

$ArtifactRoot = (Resolve-Path $ArtifactRoot).Path
$CELLSRoot = (Resolve-Path $CELLSRoot).Path

. (Join-Path $CELLSRoot "scripts\lib\CELLS-endpoints.ps1")
if ([string]::IsNullOrWhiteSpace($OutputPath)) {
    $OutputPath = Join-Path $ArtifactRoot "SMOKE_RESULTS.json"
}

$results = [ordered]@{}

function Add-SmokeResult {
    param(
        [string]$Name,
        [bool]$OK,
        [string]$Output,
        [int]$ExitCode = 0
    )
    $results[$Name] = [ordered]@{
        ok       = $OK
        output   = $Output
        exitCode = $ExitCode
    }
}

function Invoke-NativeSmoke {
    param(
        [string]$Name,
        [string]$Exe,
        [string[]]$Args,
        [string]$ExpectedText = ""
    )
    Write-Host "SMOKE: $Name"
    try {
        $output = & $Exe @Args 2>&1 | Out-String
        $ok = ($LASTEXITCODE -eq 0)
        if (-not [string]::IsNullOrWhiteSpace($ExpectedText)) {
            $ok = $ok -and ($output -match [regex]::Escape($ExpectedText))
        }
        Add-SmokeResult -Name $Name -OK $ok -Output $output.Trim() -ExitCode $LASTEXITCODE
    } catch {
        Add-SmokeResult -Name $Name -OK $false -Output $_.Exception.Message -ExitCode 1
    }
}

function Set-ScopedEnv {
    param([hashtable]$Values)
    $previous = @{}
    foreach ($key in $Values.Keys) {
        $previous[$key] = [Environment]::GetEnvironmentVariable($key, "Process")
        [Environment]::SetEnvironmentVariable($key, [string]$Values[$key], "Process")
    }
    return $previous
}

function Restore-ScopedEnv {
    param([hashtable]$Previous)
    foreach ($key in $Previous.Keys) {
        [Environment]::SetEnvironmentVariable($key, $Previous[$key], "Process")
    }
}

function Invoke-LocalGuiSmoke {
    Write-Host "SMOKE: CELLS-local-gui snapshot"
    $exe = Join-Path $ArtifactRoot "CELLS-local-gui.exe"
    if (-not (Test-Path -LiteralPath $exe)) {
        Add-SmokeResult -Name "CELLS-local-gui snapshot" -OK $false -Output "missing $exe" -ExitCode 1
        return
    }

    $id = [guid]::NewGuid().ToString("N")
    $urlFile = Join-Path $ArtifactRoot "local-gui-smoke-$id.url"
    $outLog = Join-Path $ArtifactRoot "local-gui-smoke-$id.out.log"
    $errLog = Join-Path $ArtifactRoot "local-gui-smoke-$id.err.log"
    Remove-Item -LiteralPath $urlFile, $outLog, $errLog -Force -ErrorAction SilentlyContinue

    $envSnapshot = Set-ScopedEnv @{
        CELLS_LOCAL_GUI_NO_OPEN   = "1"
        CELLS_LOCAL_GUI_STAY_OPEN = "1"
        CELLS_LOCAL_GUI_URL_FILE  = $urlFile
        HTTP_PROXY               = ""
        HTTPS_PROXY              = ""
        ALL_PROXY                = ""
        NO_PROXY                 = Get-CELLSNoProxyList -EndpointValues @(
            "http://127.0.0.1:8080",
            (Get-CELLSEndpointValue -Name "core_api_base" -EnvVar "CELLS_CHAIN_SYNC_URLS"),
            (Get-CELLSEndpointValue -Name "home_gateway_relay" -EnvVar "CELLS_HOME_GATEWAY_RELAY")
        )
    }

    $process = $null
    try {
        $process = Start-Process `
            -FilePath $exe `
            -WorkingDirectory $CELLSRoot `
            -WindowStyle Hidden `
            -RedirectStandardOutput $outLog `
            -RedirectStandardError $errLog `
            -PassThru

        $deadline = (Get-Date).AddSeconds(12)
        while (-not (Test-Path -LiteralPath $urlFile)) {
            if ((Get-Date) -gt $deadline) {
                throw "local GUI did not write URL file"
            }
            Start-Sleep -Milliseconds 100
        }

        $url = (Get-Content -LiteralPath $urlFile -Raw).Trim()
        if ($url -notmatch "[?&]t=([^&]+)") {
            throw "local GUI URL did not include a token: $url"
        }
        $token = $matches[1]
        $uri = [Uri]$url
        $base = "$($uri.Scheme)://$($uri.Authority)"

        $snapshot = $null
        $lastError = ""
        while ((Get-Date) -lt $deadline -and $null -eq $snapshot) {
            try {
                $snapshot = Invoke-RestMethod `
                    -Uri "$base/api/snapshot" `
                    -Headers @{ "X-CELLS-Token" = $token } `
                    -TimeoutSec 5
            } catch {
                $lastError = $_.Exception.Message
                Start-Sleep -Milliseconds 250
            }
        }
        if ($null -eq $snapshot) {
            throw "local GUI snapshot failed: $lastError"
        }

        try {
            Invoke-RestMethod `
                -Uri "$base/api/quit" `
                -Method Post `
                -Headers @{ "X-CELLS-Token" = $token } `
                -TimeoutSec 5 | Out-Null
        } catch {
        }

        Add-SmokeResult `
            -Name "CELLS-local-gui snapshot" `
            -OK $true `
            -Output "gui_version=$($snapshot.version); elevated=$($snapshot.elevated); exposure=$($snapshot.exposure.summary)"
    } catch {
        $stdout = Get-Content -LiteralPath $outLog -Raw -ErrorAction SilentlyContinue
        $stderr = Get-Content -LiteralPath $errLog -Raw -ErrorAction SilentlyContinue
        $details = $_.Exception.Message
        if (-not [string]::IsNullOrWhiteSpace($stdout)) {
            $details += "`nstdout: $($stdout.Trim())"
        }
        if (-not [string]::IsNullOrWhiteSpace($stderr)) {
            $details += "`nstderr: $($stderr.Trim())"
        }
        Add-SmokeResult -Name "CELLS-local-gui snapshot" -OK $false -Output $details -ExitCode 1
    } finally {
        if ($process -and -not $process.HasExited) {
            Stop-Process -Id $process.Id -Force -ErrorAction SilentlyContinue
        }
        Restore-ScopedEnv $envSnapshot
    }
}

Invoke-NativeSmoke `
    -Name "CELLS-home-gateway --version" `
    -Exe (Join-Path $ArtifactRoot "CELLS-home-gateway.exe") `
    -Args @("--version") `
    -ExpectedText "CELLS-home-gateway"

Invoke-NativeSmoke `
    -Name "CELLS-attester --version" `
    -Exe (Join-Path $ArtifactRoot "CELLS-attester.exe") `
    -Args @("--version") `
    -ExpectedText "CELLS-attester"

Invoke-NativeSmoke `
    -Name "CELLSminer --version" `
    -Exe (Join-Path $ArtifactRoot "CELLSminer.exe") `
    -Args @("--version") `
    -ExpectedText "CELLS-core-local"

Invoke-NativeSmoke `
    -Name "CELLSminer --self-test" `
    -Exe (Join-Path $ArtifactRoot "CELLSminer.exe") `
    -Args @("--self-test") `
    -ExpectedText "self-test OK"

try {
    Write-Host "SMOKE: CELLS-home-gateway --generate-key"
    $key = & (Join-Path $ArtifactRoot "CELLS-home-gateway.exe") --generate-key
    Add-SmokeResult `
        -Name "CELLS-home-gateway --generate-key" `
        -OK (($LASTEXITCODE -eq 0) -and ($key.Trim().Length -eq 64)) `
        -Output "generated 32-byte hex key" `
        -ExitCode $LASTEXITCODE
} catch {
    Add-SmokeResult -Name "CELLS-home-gateway --generate-key" -OK $false -Output $_.Exception.Message -ExitCode 1
}

Invoke-NativeSmoke `
    -Name "CELLScli help" `
    -Exe (Join-Path $ArtifactRoot "CELLScli.exe") `
    -Args @("help") `
    -ExpectedText "CELLS CLI"

Invoke-LocalGuiSmoke

$results | ConvertTo-Json -Depth 8 | Set-Content -LiteralPath $OutputPath -Encoding UTF8

$failed = @($results.GetEnumerator() | Where-Object { -not $_.Value.ok })
if ($failed.Count -gt 0) {
    foreach ($item in $failed) {
        Write-Host "$($item.Key): $($item.Value.output)"
    }
    exit 1
}

Write-Host "All smoke checks passed. Results: $OutputPath"
