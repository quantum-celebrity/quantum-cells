param(
    [string]$Version = "",
    [switch]$SkipCuda,
    [switch]$KeepGeneratedResource
)

$ErrorActionPreference = "Stop"
Set-StrictMode -Version Latest

$RepoRoot = Split-Path -Parent (Split-Path -Parent $PSScriptRoot)
$SourceRoot = Join-Path $RepoRoot "CELLS\source"
$AppsRoot = Join-Path $RepoRoot "apps\CELLS-edge-agent"
$VersionFile = Join-Path $AppsRoot "VERSION"
$HiveRoot = Join-Path $RepoRoot "apps\CELLS-hive\CELLS-hive-main"
$HiveWindowsNative = Join-Path $HiveRoot "native\windows\x64"
$HiveLinuxNative = Join-Path $HiveRoot "native\linux\x64"
$GoExe = "C:\Program Files\Go\bin\go.exe"
$NvccExe = "C:\Program Files\NVIDIA GPU Computing Toolkit\CUDA\v12.9\bin\nvcc.exe"
$VsWhereExe = "C:\Program Files (x86)\Microsoft Visual Studio\Installer\vswhere.exe"
$ControlIcon = Join-Path $HiveRoot "assets\icon.ico"
$ControlResourceBuilder = Join-Path $RepoRoot "CELLS\scripts\build_edge_control_windows_resource.ps1"
$ControlResource = Join-Path $SourceRoot "cmd\CELLS-edge-control\rsrc_windows_amd64.syso"

if (-not $Version) {
    if (-not (Test-Path -LiteralPath $VersionFile)) {
        throw "Edge-agent version file not found at $VersionFile"
    }
    $Version = (Get-Content -Raw -LiteralPath $VersionFile).Trim()
}
if ($Version -notmatch '^\d+\.\d+\.\d+$') {
    throw "Edge-agent version must use MAJOR.MINOR.PATCH format"
}

if (-not (Test-Path -LiteralPath $GoExe)) {
    throw "Go toolchain not found at $GoExe"
}

New-Item -ItemType Directory -Force -Path $AppsRoot | Out-Null
New-Item -ItemType Directory -Force -Path $HiveWindowsNative | Out-Null
New-Item -ItemType Directory -Force -Path $HiveLinuxNative | Out-Null

$previousGoOS = $env:GOOS
$previousGoArch = $env:GOARCH
$previousCgo = $env:CGO_ENABLED
$previousGoCache = $env:GOCACHE
$previousGoRoot = $env:GOROOT
& $ControlResourceBuilder -Version $Version -IconPath $ControlIcon -OutputPath $ControlResource
Push-Location $SourceRoot
try {
	$env:GOROOT = Split-Path -Parent (Split-Path -Parent $GoExe)
    $env:GOCACHE = Join-Path $RepoRoot ".cache\go-build-edge-agent"
    $env:CGO_ENABLED = "0"
    $env:GOOS = "windows"
    $env:GOARCH = "amd64"
    & $GoExe build -trimpath -ldflags "-s -w -X main.version=$Version" -o (Join-Path $HiveWindowsNative "CELLS-edge-agent.exe") ./cmd/CELLS-edge-agent
    if ($LASTEXITCODE -ne 0) {
        throw "Windows edge-agent build failed with exit code $LASTEXITCODE"
    }
    & $GoExe build -trimpath -ldflags "-s -w -H=windowsgui -X main.version=$Version" -o (Join-Path $HiveWindowsNative "CELLS-edge-control.exe") ./cmd/CELLS-edge-control
    if ($LASTEXITCODE -ne 0) {
        throw "Windows edge-control build failed with exit code $LASTEXITCODE"
    }

    $env:GOOS = "linux"
    & $GoExe build -trimpath -ldflags "-s -w -X main.version=$Version" -o (Join-Path $HiveLinuxNative "CELLS-edge-agent") ./cmd/CELLS-edge-agent
    if ($LASTEXITCODE -ne 0) {
        throw "Linux edge-agent build failed with exit code $LASTEXITCODE"
    }
    & $GoExe build -trimpath -ldflags "-s -w -X main.version=$Version" -o (Join-Path $HiveLinuxNative "CELLS-edge-control") ./cmd/CELLS-edge-control
    if ($LASTEXITCODE -ne 0) {
        throw "Linux edge-control build failed with exit code $LASTEXITCODE"
    }
} finally {
    $env:GOOS = $previousGoOS
    $env:GOARCH = $previousGoArch
    $env:CGO_ENABLED = $previousCgo
    $env:GOCACHE = $previousGoCache
	$env:GOROOT = $previousGoRoot
    if (-not $KeepGeneratedResource) {
        for ($attempt = 0; $attempt -lt 30 -and (Test-Path -LiteralPath $ControlResource); $attempt++) {
            Remove-Item -LiteralPath $ControlResource -Force -ErrorAction SilentlyContinue
            if (Test-Path -LiteralPath $ControlResource) {
                Start-Sleep -Milliseconds ([Math]::Min(($attempt + 1) * 100, 1000))
            }
        }
    }
    $resourceCleanupFailed = -not $KeepGeneratedResource -and (Test-Path -LiteralPath $ControlResource)
    Pop-Location
    if ($resourceCleanupFailed) {
        Write-Warning "Windows retained the generated Edge Control resource after cleanup retries. It is ignored by git and will be overwritten by the next build: $ControlResource"
    }
}

if (-not $SkipCuda) {
    if (-not (Test-Path -LiteralPath $NvccExe)) {
        throw "CUDA compiler not found at $NvccExe. Use -SkipCuda only for a CPU/RAM-only package."
    }
    if (-not (Test-Path -LiteralPath $VsWhereExe)) {
        throw "Visual Studio discovery tool not found at $VsWhereExe"
    }
    $visualStudioPath = (& $VsWhereExe -latest -products * -requires Microsoft.VisualStudio.Component.VC.Tools.x86.x64 -property installationPath).Trim()
    if (-not $visualStudioPath) {
        throw "Visual C++ x64 build tools are required for the CUDA helper"
    }
    $compiler = Get-ChildItem (Join-Path $visualStudioPath "VC\Tools\MSVC") -Recurse -Filter cl.exe |
        Where-Object { $_.FullName -match 'Hostx64\\x64\\cl\.exe$' } |
        Select-Object -First 1
    if ($null -eq $compiler) {
        throw "Visual C++ x64 compiler was not found below $visualStudioPath"
    }
    $cudaSource = Join-Path $SourceRoot "cmd\CELLS-edge-gpu-helper\main.cu"
    $cudaBuildDir = Join-Path $RepoRoot ".cache\edgepool-cuda"
    New-Item -ItemType Directory -Force -Path $cudaBuildDir | Out-Null
    $cudaOutput = Join-Path $cudaBuildDir "CELLS-edge-gpu-helper.exe"
    & $NvccExe -O3 -std=c++17 -arch=sm_75 -allow-unsupported-compiler -ccbin $compiler.DirectoryName $cudaSource -o $cudaOutput
    if ($LASTEXITCODE -ne 0) {
        throw "CUDA edge helper build failed with exit code $LASTEXITCODE"
    }
    Copy-Item -LiteralPath $cudaOutput -Destination (Join-Path $HiveWindowsNative "CELLS-edge-gpu-helper.exe") -Force
}

Copy-Item -LiteralPath (Join-Path $HiveWindowsNative "CELLS-edge-agent.exe") -Destination (Join-Path $AppsRoot "CELLS-edge-agent-windows-x86_64.exe") -Force
Copy-Item -LiteralPath (Join-Path $HiveLinuxNative "CELLS-edge-agent") -Destination (Join-Path $AppsRoot "CELLS-edge-agent-linux-x86_64") -Force
Copy-Item -LiteralPath (Join-Path $HiveWindowsNative "CELLS-edge-control.exe") -Destination (Join-Path $AppsRoot "CELLS-edge-control-windows-x86_64.exe") -Force
Copy-Item -LiteralPath (Join-Path $HiveLinuxNative "CELLS-edge-control") -Destination (Join-Path $AppsRoot "CELLS-edge-control-linux-x86_64") -Force
if (Test-Path -LiteralPath (Join-Path $HiveWindowsNative "CELLS-edge-gpu-helper.exe")) {
    Copy-Item -LiteralPath (Join-Path $HiveWindowsNative "CELLS-edge-gpu-helper.exe") -Destination (Join-Path $AppsRoot "CELLS-edge-gpu-helper-windows-x86_64.exe") -Force
}

$AppLinuxHelper = Join-Path $AppsRoot "CELLS-edge-gpu-helper-linux-x86_64"
if (Test-Path -LiteralPath $AppLinuxHelper) {
    Copy-Item -LiteralPath $AppLinuxHelper -Destination (Join-Path $HiveLinuxNative "CELLS-edge-gpu-helper") -Force
}

$DownloadsRoot = Join-Path $RepoRoot "CELLS\deploy\landing\downloads"
$StageRoot = Join-Path $RepoRoot (".cache\edgepool-release\{0}-{1}" -f $Version, [System.Guid]::NewGuid().ToString("N"))
$WindowsStage = Join-Path $StageRoot "windows"
$LinuxFolderName = "CELLS-edge-agent-$Version-linux-x86_64"
$LinuxStage = Join-Path $StageRoot $LinuxFolderName
$Readme = Join-Path $AppsRoot "README.md"
$WindowsAgent = Join-Path $AppsRoot "CELLS-edge-agent-windows-x86_64.exe"
$WindowsHelper = Join-Path $AppsRoot "CELLS-edge-gpu-helper-windows-x86_64.exe"
$LinuxAgent = Join-Path $AppsRoot "CELLS-edge-agent-linux-x86_64"
$LinuxHelper = Join-Path $AppsRoot "CELLS-edge-gpu-helper-linux-x86_64"
$WindowsControl = Join-Path $AppsRoot "CELLS-edge-control-windows-x86_64.exe"
$LinuxControl = Join-Path $AppsRoot "CELLS-edge-control-linux-x86_64"
foreach ($required in @($Readme, $WindowsAgent, $WindowsHelper, $WindowsControl, $LinuxAgent, $LinuxHelper, $LinuxControl)) {
    if (-not (Test-Path -LiteralPath $required)) {
        throw "Required edge-agent release input is missing: $required"
    }
}

New-Item -ItemType Directory -Force -Path $WindowsStage, $LinuxStage, $DownloadsRoot | Out-Null

Copy-Item -LiteralPath $WindowsAgent -Destination (Join-Path $WindowsStage "CELLS-edge-agent.exe")
Copy-Item -LiteralPath $WindowsHelper -Destination (Join-Path $WindowsStage "CELLS-edge-gpu-helper.exe")
Copy-Item -LiteralPath $WindowsControl -Destination (Join-Path $WindowsStage "CELLS Edge Control.exe")
Copy-Item -LiteralPath $Readme -Destination (Join-Path $WindowsStage "README.md")
Copy-Item -LiteralPath $LinuxAgent -Destination (Join-Path $LinuxStage "CELLS-edge-agent")
Copy-Item -LiteralPath $LinuxHelper -Destination (Join-Path $LinuxStage "CELLS-edge-gpu-helper")
Copy-Item -LiteralPath $LinuxControl -Destination (Join-Path $LinuxStage "CELLS-edge-control")
Copy-Item -LiteralPath $Readme -Destination (Join-Path $LinuxStage "README.md")

$WindowsArchive = Join-Path $DownloadsRoot "CELLS-edge-agent-$Version-windows-x86_64.zip"
$LinuxArchive = Join-Path $DownloadsRoot "CELLS-edge-agent-$Version-linux-x86_64.tar.gz"
$WindowsArchiveStage = Join-Path $StageRoot "CELLS-edge-agent-$Version-windows-x86_64.zip"
$LinuxArchiveStage = Join-Path $StageRoot "CELLS-edge-agent-$Version-linux-x86_64.tar.gz"
Compress-Archive -Path (Join-Path $WindowsStage "*") -DestinationPath $WindowsArchiveStage -Force
$previousPackageCache = $env:GOCACHE
$previousPackageGoOS = $env:GOOS
$previousPackageGoArch = $env:GOARCH
$previousPackageCgo = $env:CGO_ENABLED
$env:GOCACHE = Join-Path $RepoRoot ".cache\go-build-edge-agent"
$env:GOOS = "windows"
$env:GOARCH = "amd64"
$env:CGO_ENABLED = "0"
Push-Location $SourceRoot
try {
    & $GoExe run ./cmd/CELLS-edge-agent-package --source $LinuxStage --output $LinuxArchiveStage --root $LinuxFolderName
    if ($LASTEXITCODE -ne 0) {
        throw "Linux edge-agent archive failed with exit code $LASTEXITCODE"
    }
} finally {
    Pop-Location
    $env:GOCACHE = $previousPackageCache
    $env:GOOS = $previousPackageGoOS
    $env:GOARCH = $previousPackageGoArch
    $env:CGO_ENABLED = $previousPackageCgo
}
Copy-Item -LiteralPath $WindowsArchiveStage -Destination $WindowsArchive -Force
Copy-Item -LiteralPath $LinuxArchiveStage -Destination $LinuxArchive -Force

$PublishedFiles = @(
    @{ Source = $WindowsAgent; Name = "CELLS-edge-agent-$Version-windows-x86_64.exe" },
    @{ Source = $WindowsHelper; Name = "CELLS-edge-gpu-helper-$Version-windows-x86_64.exe" },
    @{ Source = $WindowsControl; Name = "CELLS-edge-control-$Version-windows-x86_64.exe" },
    @{ Source = $LinuxAgent; Name = "CELLS-edge-agent-$Version-linux-x86_64" },
    @{ Source = $LinuxHelper; Name = "CELLS-edge-gpu-helper-$Version-linux-x86_64" },
    @{ Source = $LinuxControl; Name = "CELLS-edge-control-$Version-linux-x86_64" }
)
foreach ($entry in $PublishedFiles) {
    Copy-Item -LiteralPath $entry.Source -Destination (Join-Path $DownloadsRoot $entry.Name) -Force
}
$ChecksumTargets = @($WindowsArchive, $LinuxArchive) + ($PublishedFiles | ForEach-Object { Join-Path $DownloadsRoot $_.Name })
$ChecksumLines = foreach ($target in $ChecksumTargets) {
    $hash = (Get-FileHash -Algorithm SHA256 -LiteralPath $target).Hash
    "$hash  $([System.IO.Path]::GetFileName($target))"
}
$ChecksumPath = Join-Path $DownloadsRoot "CELLS-edge-agent-$Version-SHA256SUMS.txt"
[System.IO.File]::WriteAllLines($ChecksumPath, $ChecksumLines, [System.Text.UTF8Encoding]::new($false))

Write-Host "CELLS edge-agent $Version binaries built in $AppsRoot"
Write-Host "Release bundles and checksums written to $DownloadsRoot"
