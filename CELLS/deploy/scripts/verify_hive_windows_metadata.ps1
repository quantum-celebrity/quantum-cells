[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [string]$UnpackedDirectory
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

$unpackedRoot = (Resolve-Path -LiteralPath $UnpackedDirectory).Path
$hiveRoot = (Resolve-Path (Join-Path $PSScriptRoot '..\..\..\apps\CELLS-hive\CELLS-hive-main')).Path
$hiveVersion = (Get-Content -Raw (Join-Path $hiveRoot 'release\app\package.json') | ConvertFrom-Json).version
if ($hiveVersion -notmatch '^(\d+\.\d+\.\d+)(?:-[0-9A-Za-z.-]+)?$') {
    throw 'Hive version must use SemVer MAJOR.MINOR.PATCH with an optional prerelease suffix.'
}
$hiveBinaryVersion = $Matches[1]
$edgeVersion = (Get-Content -Raw (Join-Path $PSScriptRoot '..\..\..\apps\CELLS-edge-agent\VERSION')).Trim()

$files = @(
    @{ Path = 'CELLS Hive.exe'; Description = 'CELLS Hive'; FileVersion = $hiveBinaryVersion; Original = '' },
    @{ Path = 'resources\edge\CELLS-edge-agent.exe'; Description = 'CELLS Edge Agent'; FileVersion = $edgeVersion; Original = 'CELLS-edge-agent.exe' },
    @{ Path = 'resources\edge\CELLS-edge-control.exe'; Description = 'CELLS Edge Control'; FileVersion = $edgeVersion; Original = 'CELLS-edge-control.exe' },
    @{ Path = 'resources\edge\CELLS-edge-gpu-helper.exe'; Description = 'CELLS Edge GPU Helper'; FileVersion = $hiveBinaryVersion; Original = 'CELLS-edge-gpu-helper.exe' },
    @{ Path = 'resources\native\CELLScli.exe'; Description = 'CELLS Command Line Interface'; FileVersion = $hiveBinaryVersion; Original = 'CELLScli.exe' },
    @{ Path = 'resources\native\CELLS-hive-wallet-host.exe'; Description = 'CELLS Hive Wallet Browser Bridge'; FileVersion = $hiveBinaryVersion; Original = 'CELLS-hive-wallet-host.exe' },
    @{ Path = 'resources\miner\CELLSminer-console.exe'; Description = 'CELLS Console Miner'; FileVersion = $hiveBinaryVersion; Original = 'CELLSminer-console.exe' },
    @{ Path = 'resources\miner\CELLS-miner-cuda-solver.exe'; Description = 'CELLS CUDA Miner Solver'; FileVersion = $hiveBinaryVersion; Original = 'CELLS-miner-cuda-solver.exe' }
)

$evidence = @()
foreach ($file in $files) {
    $path = Join-Path $unpackedRoot $file.Path
    if (-not (Test-Path -LiteralPath $path -PathType Leaf)) {
        throw "Required CELLS executable is missing: $path"
    }

    $versionInfo = (Get-Item -LiteralPath $path).VersionInfo
    if ($versionInfo.ProductName -cne 'CELLS Hive') {
        throw "ProductName mismatch for $($file.Path): '$($versionInfo.ProductName)'"
    }
    if ($versionInfo.CompanyName -cne 'CELLS') {
        throw "CompanyName mismatch for $($file.Path): '$($versionInfo.CompanyName)'"
    }
    if ($versionInfo.ProductVersion -notlike "$hiveBinaryVersion*") {
        throw "ProductVersion mismatch for $($file.Path): '$($versionInfo.ProductVersion)'"
    }
    if ($versionInfo.FileVersion -notlike "$($file.FileVersion)*") {
        throw "FileVersion mismatch for $($file.Path): '$($versionInfo.FileVersion)'"
    }
    if ($versionInfo.FileDescription -cne $file.Description) {
        throw "FileDescription mismatch for $($file.Path): '$($versionInfo.FileDescription)'"
    }
    if ($file.Original -and $versionInfo.OriginalFilename -cne $file.Original) {
        throw "OriginalFilename mismatch for $($file.Path): '$($versionInfo.OriginalFilename)'"
    }

    $evidence += [ordered]@{
        path = $file.Path.Replace('\', '/')
        product_name = $versionInfo.ProductName
        product_version = $versionInfo.ProductVersion
        file_version = $versionInfo.FileVersion
        company_name = $versionInfo.CompanyName
        file_description = $versionInfo.FileDescription
        original_filename = $versionInfo.OriginalFilename
        sha256 = (Get-FileHash -Algorithm SHA256 -LiteralPath $path).Hash.ToLowerInvariant()
    }
}

$result = [ordered]@{
    schema = 'CELLS.windows-metadata-evidence.v1'
    generated_at = (Get-Date).ToUniversalTime().ToString('o')
    hive_version = $hiveVersion
    edge_version = $edgeVersion
    files = $evidence
}
$evidencePath = Join-Path (Split-Path $unpackedRoot -Parent) 'windows-metadata-evidence.json'
$result | ConvertTo-Json -Depth 6 | Set-Content -LiteralPath $evidencePath -Encoding UTF8

Write-Host "Verified Windows metadata for $($files.Count) CELLS Hive executables."
Write-Host "Evidence: $evidencePath"
