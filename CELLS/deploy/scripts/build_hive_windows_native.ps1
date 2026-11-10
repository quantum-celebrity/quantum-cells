param(
    [string]$HiveSourceDir = "apps/CELLS-hive/CELLS-hive-main",
    [string]$CELLSSourceDir = "CELLS/source",
    [string]$EdgeAgentVersion = "",
    [string]$GoExe = "",
    [switch]$KeepGeneratedResource,
    [switch]$SkipCudaRuntimeSelfTest
)

$ErrorActionPreference = 'Stop'

$workspace = (Resolve-Path (Join-Path $PSScriptRoot '..\..\..')).Path
$hive = (Resolve-Path (Join-Path $workspace $HiveSourceDir)).Path
$CELLS = (Resolve-Path (Join-Path $workspace $CELLSSourceDir)).Path
$native = Join-Path $hive 'native\windows\x64'
$goCache = Join-Path $workspace '.cache\go-build'
$goModCache = Join-Path $workspace '.cache\go-mod'
$officialGo = Join-Path $env:ProgramFiles 'Go\bin\go.exe'
$goOverride = if ($GoExe) { $GoExe } else { $env:CELLS_GO_EXE }
$go = if ($goOverride) {
    if (-not (Test-Path -LiteralPath $goOverride -PathType Leaf)) {
        throw "Configured Go executable was not found: $goOverride"
    }
    (Resolve-Path -LiteralPath $goOverride).Path
} elseif (Test-Path -LiteralPath $officialGo) {
    $officialGo
} else {
    (Get-Command go -ErrorAction Stop).Source
}
$requiredGoLine = Get-Content -LiteralPath (Join-Path $CELLS 'go.mod') |
    Where-Object { $_ -match '^go\s+(\d+\.\d+\.\d+)\s*$' } |
    Select-Object -First 1
if (-not $requiredGoLine) {
    throw 'CELLS go.mod does not contain a MAJOR.MINOR.PATCH go directive.'
}
$requiredGo = [version]([regex]::Match($requiredGoLine, '^go\s+(\d+\.\d+\.\d+)\s*$').Groups[1].Value)
$env:GOTOOLCHAIN = 'auto'
$env:GOMODCACHE = $goModCache
New-Item -ItemType Directory -Force -Path $goModCache | Out-Null
Push-Location $CELLS
try {
    $goVersionOutput = (& $go env GOVERSION).Trim()
    if ($LASTEXITCODE -ne 0 -or $goVersionOutput -notmatch '^go(\d+\.\d+\.\d+)$') {
        throw "Unable to select the Go toolchain required by $CELLS\go.mod using $go"
    }
    $goVersion = [version]$Matches[1]
}
finally {
    Pop-Location
}
if ($goVersion -lt $requiredGo) {
    throw "Go $requiredGo or newer is required; automatic toolchain selection returned $goVersion from $go. Set CELLS_GO_EXE to a current Go SDK."
}
Write-Host "Using $goVersionOutput through $go"
$packageVersion = (Get-Content -Raw (Join-Path $hive 'package.json') | ConvertFrom-Json).version
$version = (Get-Content -Raw (Join-Path $hive 'release\app\package.json') | ConvertFrom-Json).version
if ($packageVersion -ne $version) {
    throw "Hive package version $packageVersion does not match runtime version $version. Update both manifests before packaging."
}
if ($version -notmatch '^(\d+\.\d+\.\d+)(?:-[0-9A-Za-z.-]+)?$') {
    throw 'Hive version must use SemVer MAJOR.MINOR.PATCH with an optional prerelease suffix.'
}
$binaryVersion = $Matches[1]
$edgeVersionFile = Join-Path $workspace 'apps\CELLS-edge-agent\VERSION'
if (-not $EdgeAgentVersion) {
    if (-not (Test-Path -LiteralPath $edgeVersionFile)) {
        throw "CELLS edge-agent version file not found at $edgeVersionFile"
    }
    $EdgeAgentVersion = (Get-Content -Raw -LiteralPath $edgeVersionFile).Trim()
}
if ($EdgeAgentVersion -notmatch '^\d+\.\d+\.\d+$') {
    throw 'EdgeAgentVersion must use MAJOR.MINOR.PATCH format.'
}
$gitSha = (& git -C $CELLS rev-parse --short HEAD).Trim()
$buildDate = (Get-Date).ToUniversalTime().ToString('yyyy-MM-ddTHH:mm:ssZ')
$buildInfo = "-s -w -X github.com/blackbeardONE/CELLS/pkg/buildinfo.Version=hive-v$version -X github.com/blackbeardONE/CELLS/pkg/buildinfo.GitSHA=$gitSha -X github.com/blackbeardONE/CELLS/pkg/buildinfo.BuildDate=$buildDate"
$controlIcon = Join-Path $hive 'assets\icon.ico'
$versionResourceBuilder = Join-Path $workspace 'CELLS\scripts\build_windows_version_resource.ps1'
$cudaSolverBuilder = Join-Path $workspace 'CELLS\scripts\build_miner_cuda.ps1'
$edgeGpuBuilder = Join-Path $workspace 'CELLS\scripts\build_edge_gpu_helper.ps1'
$cudaSolver = Join-Path $native 'CELLS-miner-cuda-solver.exe'
$edgeGpuHelper = Join-Path $native 'CELLS-edge-gpu-helper.exe'

$resourceSpecs = @(
    @{
        Path = Join-Path $CELLS 'cmd\CELLScli\rsrc_windows_amd64.syso'
        FileVersion = $binaryVersion
        Description = 'CELLS Command Line Interface'
        InternalName = 'CELLScli'
        OriginalFilename = 'CELLScli.exe'
    },
    @{
        Path = Join-Path $CELLS 'cmd\CELLSminer-console\rsrc_windows_amd64.syso'
        FileVersion = $binaryVersion
        Description = 'CELLS Console Miner'
        InternalName = 'CELLSminer-console'
        OriginalFilename = 'CELLSminer-console.exe'
    },
    @{
        Path = Join-Path $CELLS 'cmd\CELLS-hive-wallet-host\rsrc_windows_amd64.syso'
        FileVersion = $binaryVersion
        Description = 'CELLS Hive Wallet Browser Bridge'
        InternalName = 'CELLS-hive-wallet-host'
        OriginalFilename = 'CELLS-hive-wallet-host.exe'
    },
    @{
        Path = Join-Path $CELLS 'cmd\CELLS-edge-agent\rsrc_windows_amd64.syso'
        FileVersion = $EdgeAgentVersion
        Description = 'CELLS Edge Agent'
        InternalName = 'CELLS-edge-agent'
        OriginalFilename = 'CELLS-edge-agent.exe'
    },
    @{
        Path = Join-Path $CELLS 'cmd\CELLS-edge-control\rsrc_windows_amd64.syso'
        FileVersion = $EdgeAgentVersion
        Description = 'CELLS Edge Control'
        InternalName = 'CELLS-edge-control'
        OriginalFilename = 'CELLS-edge-control.exe'
    }
)

New-Item -ItemType Directory -Force -Path $native | Out-Null
New-Item -ItemType Directory -Force -Path $goCache | Out-Null

foreach ($resource in $resourceSpecs) {
    & $versionResourceBuilder `
        -ProductVersion $binaryVersion `
        -FileVersion $resource.FileVersion `
        -ProductName 'CELLS Hive' `
        -FileDescription $resource.Description `
        -InternalName $resource.InternalName `
        -OriginalFilename $resource.OriginalFilename `
        -IconPath $controlIcon `
        -OutputPath $resource.Path
}
Push-Location $CELLS
try {
    $env:GOCACHE = $goCache
    $env:CGO_ENABLED = '0'
    $env:GOOS = 'windows'
    $env:GOARCH = 'amd64'

    & $go build -trimpath -tags dilithium_circl -ldflags '-s -w' -o (Join-Path $native 'CELLScli.exe') ./cmd/CELLScli
    if ($LASTEXITCODE -ne 0) { throw "CELLScli build failed with exit code $LASTEXITCODE" }

    & $go build -trimpath -ldflags '-s -w -H=windowsgui' -o (Join-Path $native 'CELLS-hive-wallet-host.exe') ./cmd/CELLS-hive-wallet-host
    if ($LASTEXITCODE -ne 0) { throw "CELLS-hive-wallet-host build failed with exit code $LASTEXITCODE" }

    & $go build -trimpath -ldflags $buildInfo -o (Join-Path $native 'CELLSminer-console.exe') ./cmd/CELLSminer-console
    if ($LASTEXITCODE -ne 0) { throw "CELLSminer-console build failed with exit code $LASTEXITCODE" }

    & $go build -trimpath -ldflags "-s -w -X main.version=$EdgeAgentVersion" -o (Join-Path $native 'CELLS-edge-agent.exe') ./cmd/CELLS-edge-agent
    if ($LASTEXITCODE -ne 0) { throw "CELLS-edge-agent build failed with exit code $LASTEXITCODE" }

    & $go build -trimpath -ldflags "-s -w -H=windowsgui -X main.version=$EdgeAgentVersion" -o (Join-Path $native 'CELLS-edge-control.exe') ./cmd/CELLS-edge-control
    if ($LASTEXITCODE -ne 0) { throw "CELLS-edge-control build failed with exit code $LASTEXITCODE" }
}
finally {
    $resourceCleanupFailed = @()
    if (-not $KeepGeneratedResource) {
        foreach ($resource in $resourceSpecs) {
            for ($attempt = 0; $attempt -lt 30 -and (Test-Path -LiteralPath $resource.Path); $attempt++) {
                Remove-Item -LiteralPath $resource.Path -Force -ErrorAction SilentlyContinue
                if (Test-Path -LiteralPath $resource.Path) {
                    Start-Sleep -Milliseconds ([Math]::Min(($attempt + 1) * 100, 1000))
                }
            }
            if (Test-Path -LiteralPath $resource.Path) {
                $resourceCleanupFailed += $resource.Path
            }
        }
    }
    Pop-Location
    if ($resourceCleanupFailed.Count -gt 0) {
        Write-Warning "Windows retained generated version resources after cleanup retries. They are ignored by git and will be overwritten by the next build: $($resourceCleanupFailed -join ', ')"
    }
}

$cudaArguments = @{
    Version = $binaryVersion
    SkipRuntimeSelfTest = [bool]$SkipCudaRuntimeSelfTest
}
& $cudaSolverBuilder @cudaArguments
if ($LASTEXITCODE -ne 0) { throw 'CELLS CUDA miner solver build failed.' }

& $edgeGpuBuilder @cudaArguments
if ($LASTEXITCODE -ne 0) { throw 'CELLS Edge GPU Helper build failed.' }

& (Join-Path $native 'CELLSminer-console.exe') --version
if ($LASTEXITCODE -ne 0) { throw 'Packaged CELLSminer-console failed its version probe.' }

if (-not $SkipCudaRuntimeSelfTest) {
    & $cudaSolver --self-test
    if ($LASTEXITCODE -ne 0) { throw 'Packaged CELLS CUDA miner solver failed its self-test.' }

    $gpuResult = & $edgeGpuHelper --seed ('00' * 32) --units 1024 --json | ConvertFrom-Json
    if ($LASTEXITCODE -ne 0 -or -not $gpuResult.gpu_name) {
        throw 'Packaged CELLS Edge GPU Helper failed its runtime self-test.'
    }
}

& (Join-Path $native 'CELLS-edge-agent.exe') --version
if ($LASTEXITCODE -ne 0) { throw 'Packaged CELLS-edge-agent failed its version probe.' }

& (Join-Path $native 'CELLS-edge-control.exe') --version
if ($LASTEXITCODE -ne 0) { throw 'Packaged CELLS-edge-control failed its version probe.' }

Write-Host "Hive Windows native tools are ready in $native"
