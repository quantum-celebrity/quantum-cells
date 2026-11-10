param(
    [Parameter(Mandatory = $true)]
    [string]$Version,

    [Parameter(Mandatory = $true)]
    [string]$IconPath,

    [Parameter(Mandatory = $true)]
    [string]$OutputPath
)

$ErrorActionPreference = 'Stop'

$builder = Join-Path $PSScriptRoot 'build_windows_version_resource.ps1'
& $builder `
    -ProductVersion $Version `
    -FileVersion $Version `
    -ProductName 'CELLS Hive' `
    -FileDescription 'CELLS Edge Control' `
    -InternalName 'CELLS-edge-control' `
    -OriginalFilename 'CELLS-edge-control.exe' `
    -IconPath $IconPath `
    -OutputPath $OutputPath
