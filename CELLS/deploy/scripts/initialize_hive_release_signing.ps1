param(
    [string]$StorageDirectory = "",
    [string]$CELLSCliPath = ""
)

$ErrorActionPreference = 'Stop'

if ([Environment]::OSVersion.Platform -ne [PlatformID]::Win32NT) {
    throw 'Release-key initialization currently requires Windows DPAPI.'
}

$workspace = (Resolve-Path (Join-Path $PSScriptRoot '..\..\..')).Path
if (-not $StorageDirectory) {
    $StorageDirectory = Join-Path $workspace '.cache\CELLS-release-signing'
}
$StorageDirectory = [IO.Path]::GetFullPath($StorageDirectory)
$keystorePath = Join-Path $StorageDirectory 'release-signing-wallet.json'
$protectedPassphrasePath = Join-Path $StorageDirectory 'release-signing-passphrase.dpapi'
$publicMetadataPath = Join-Path $StorageDirectory 'release-signing-public.json'
$temporaryPassphrasePath = Join-Path $StorageDirectory ('.passphrase-' + [Guid]::NewGuid().ToString('N') + '.tmp')

foreach ($path in @($keystorePath, $protectedPassphrasePath, $publicMetadataPath)) {
    if (Test-Path -LiteralPath $path) {
        throw "Refusing to replace existing release-signing material: $path"
    }
}

if (-not $CELLSCliPath) {
    $candidatePaths = @(
        (Join-Path $workspace 'apps\CELLS-hive\CELLS-hive-main\native\windows\x64\CELLScli.exe'),
        (Join-Path $workspace '.cache\CELLS-release-signing\CELLScli.exe'),
        (Join-Path $workspace 'CELLS\source\.cache\local-validator\CELLScli.exe')
    )
    $CELLSCliPath = $candidatePaths |
        Where-Object { Test-Path -LiteralPath $_ -PathType Leaf } |
        Select-Object -First 1
}
if (-not $CELLSCliPath -or -not (Test-Path -LiteralPath $CELLSCliPath -PathType Leaf)) {
    throw 'CELLScli was not found. Build it first or pass -CELLSCliPath.'
}
$CELLSCliPath = (Resolve-Path -LiteralPath $CELLSCliPath).Path

New-Item -ItemType Directory -Force -Path $StorageDirectory | Out-Null
$currentAccount = [Security.Principal.WindowsIdentity]::GetCurrent().Name
& icacls.exe $StorageDirectory `
    '/inheritance:r' `
    '/grant:r' `
    "${currentAccount}:(OI)(CI)F" `
    'BUILTIN\Administrators:(OI)(CI)F' `
    'NT AUTHORITY\SYSTEM:(OI)(CI)F' | Out-Null
if ($LASTEXITCODE -ne 0) {
    throw 'Could not restrict the release-signing directory ACL.'
}
$randomBytes = New-Object byte[] 48
[Security.Cryptography.RandomNumberGenerator]::Fill($randomBytes)
$passphrase = [Convert]::ToBase64String($randomBytes)

try {
    [IO.File]::WriteAllText(
        $temporaryPassphrasePath,
        $passphrase,
        [Text.UTF8Encoding]::new($false)
    )

    & $CELLSCliPath wallet new `
        --out $keystorePath `
        --passphrase-file $temporaryPassphrasePath | Out-Null
    if ($LASTEXITCODE -ne 0) {
        throw "CELLScli wallet new failed with exit code $LASTEXITCODE"
    }

    $protectedPassphrase = ConvertTo-SecureString $passphrase -AsPlainText -Force |
        ConvertFrom-SecureString
    [IO.File]::WriteAllText(
        $protectedPassphrasePath,
        $protectedPassphrase + [Environment]::NewLine,
        [Text.UTF8Encoding]::new($false)
    )

    $wallet = (& $CELLSCliPath wallet show --in $keystorePath --json | ConvertFrom-Json)
    if ($LASTEXITCODE -ne 0 -or -not $wallet.public_key) {
        throw 'Could not read the release-signing public key.'
    }
    $publicKeyBytes = [Convert]::FromHexString([string]$wallet.public_key)
    $keyId = [Convert]::ToHexString(
        [Security.Cryptography.SHA256]::HashData($publicKeyBytes)
    ).ToLowerInvariant()
    $metadata = [ordered]@{
        schema = 'CELLS.release-trust-key.v1'
        key_id = $keyId
        algorithm = 'ML-DSA-87'
        public_key = ([string]$wallet.public_key).ToLowerInvariant()
        address = [string]$wallet.address
        created_at = (Get-Date).ToUniversalTime().ToString('o')
        custody = 'Encrypted CELLS keystore; passphrase protected by Windows DPAPI.'
    }
    [IO.File]::WriteAllText(
        $publicMetadataPath,
        (($metadata | ConvertTo-Json -Depth 4) + [Environment]::NewLine),
        [Text.UTF8Encoding]::new($false)
    )
}
finally {
    if (Test-Path -LiteralPath $temporaryPassphrasePath) {
        Remove-Item -LiteralPath $temporaryPassphrasePath -Force
    }
    [Array]::Clear($randomBytes, 0, $randomBytes.Length)
    $passphrase = $null
}

Write-Host "Created encrypted CELLS release key in $StorageDirectory"
Write-Host "Public metadata: $publicMetadataPath"
Write-Host 'Move the directory to encrypted offline storage before production signing.'
