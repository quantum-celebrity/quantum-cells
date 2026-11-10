[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [string]$ReleaseDirectory,

    [string]$ExpectedPublisher = 'CELLS',

    [string]$EvidencePath,

    [switch]$UnpackedOnly
)

$ErrorActionPreference = 'Stop'

$securityModule = Join-Path $env:WINDIR `
    'System32\WindowsPowerShell\v1.0\Modules\Microsoft.PowerShell.Security\Microsoft.PowerShell.Security.psd1'
if (Test-Path -LiteralPath $securityModule) {
    Import-Module -Name $securityModule -Force -ErrorAction Stop
} else {
    Import-Module -Name Microsoft.PowerShell.Security -Force -ErrorAction Stop
}

$releaseRoot = (Resolve-Path -LiteralPath $ReleaseDirectory).Path
$releaseAppPackage = Join-Path (Split-Path $releaseRoot -Parent) 'app\package.json'
if (-not (Test-Path -LiteralPath $releaseAppPackage)) {
    throw "Packaged application metadata was not found: $releaseAppPackage"
}

$version = (Get-Content -Raw -LiteralPath $releaseAppPackage | ConvertFrom-Json).version
if ([string]::IsNullOrWhiteSpace($version)) {
    throw "Packaged application version is missing from $releaseAppPackage"
}

$unpackedFiles = @(
    'win-unpacked\CELLS Hive.exe'
    'win-unpacked\resources\edge\CELLS-edge-agent.exe'
    'win-unpacked\resources\edge\CELLS-edge-control.exe'
    'win-unpacked\resources\edge\CELLS-edge-gpu-helper.exe'
    'win-unpacked\resources\native\CELLScli.exe'
    'win-unpacked\resources\miner\CELLSminer-console.exe'
    'win-unpacked\resources\miner\CELLS-miner-cuda-solver.exe'
)
$requiredFiles = if ($UnpackedOnly) {
    $unpackedFiles
} else {
    @("CELLS-hive-$version-win-x64.exe") + $unpackedFiles
}

$evidenceFiles = @()
foreach ($relativePath in $requiredFiles) {
    $path = Join-Path $releaseRoot $relativePath
    if (-not (Test-Path -LiteralPath $path -PathType Leaf)) {
        throw "Required signed Windows release file was not found: $path"
    }

    $signature = Get-AuthenticodeSignature -LiteralPath $path
    if ($signature.Status -ne [System.Management.Automation.SignatureStatus]::Valid) {
        throw "Authenticode verification failed for ${relativePath}: $($signature.Status) - $($signature.StatusMessage)"
    }
    if ($null -eq $signature.SignerCertificate) {
        throw "Authenticode signer certificate is missing for $relativePath"
    }

    $codeSigningEku = $signature.SignerCertificate.EnhancedKeyUsageList |
        Where-Object { $_.ObjectId.Value -eq '1.3.6.1.5.5.7.3.3' }
    if (-not $codeSigningEku) {
        throw "Signer certificate for $relativePath is not valid for code signing"
    }

    $publisher = $signature.SignerCertificate.GetNameInfo(
        [Security.Cryptography.X509Certificates.X509NameType]::SimpleName,
        $false
    )
    if ($publisher -cne $ExpectedPublisher) {
        throw "Publisher mismatch for ${relativePath}: expected '$ExpectedPublisher', got '$publisher'"
    }
    if ($null -eq $signature.TimeStamperCertificate) {
        throw "A trusted timestamp is required for $relativePath"
    }

    $evidenceFiles += [ordered]@{
        path = $relativePath.Replace('\', '/')
        sha256 = (Get-FileHash -Algorithm SHA256 -LiteralPath $path).Hash.ToLowerInvariant()
        publisher = $publisher
        signer_thumbprint = $signature.SignerCertificate.Thumbprint
        signer_not_after = $signature.SignerCertificate.NotAfter.ToUniversalTime().ToString('o')
        timestamp_authority = $signature.TimeStamperCertificate.GetNameInfo(
            [Security.Cryptography.X509Certificates.X509NameType]::SimpleName,
            $false
        )
        timestamp_thumbprint = $signature.TimeStamperCertificate.Thumbprint
    }
}

if ([string]::IsNullOrWhiteSpace($EvidencePath)) {
    $EvidencePath = Join-Path $releaseRoot 'windows-signature-evidence.json'
}
$evidenceParent = Split-Path -Parent $EvidencePath
if ($evidenceParent) {
    New-Item -ItemType Directory -Force -Path $evidenceParent | Out-Null
}

$evidence = [ordered]@{
    schema = 'CELLS.windows-signature-evidence.v1'
    generated_at = (Get-Date).ToUniversalTime().ToString('o')
    version = $version
    scope = if ($UnpackedOnly) { 'unpacked' } else { 'release' }
    expected_publisher = $ExpectedPublisher
    files = $evidenceFiles
}
$evidence | ConvertTo-Json -Depth 6 | Set-Content -LiteralPath $EvidencePath -Encoding UTF8

Write-Host "Verified $($requiredFiles.Count) timestamped Authenticode signatures for CELLS Hive $version."
Write-Host "Evidence: $EvidencePath"
