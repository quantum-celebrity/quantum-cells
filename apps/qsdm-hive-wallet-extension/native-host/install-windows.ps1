param(
    [string]$ExtensionId = '',
    [string]$FirefoxExtensionId = 'CELLS-wallet@CELLS.tech',
    [string]$HostPath = ""
)

$ErrorActionPreference = 'Stop'
$extensionIdPattern = '^[a-p]{32}$'
$firefoxExtensionIdPattern = '^[A-Za-z0-9._+@-]{1,128}$'

# Every extension Hive trusts must be listed, because Chrome refuses a native
# messaging connection from any extension absent from allowed_origins. This
# must stay in step with CELLS_WALLET_TRUSTED_EXTENSION_IDS in
# apps/CELLS-hive/CELLS-hive-main/src/main/services/CELLSWalletProviderNativeHost.ts;
# a spec in that file asserts this script lists all of them.
#
# Writing a single ID was a real defect: a host registered by this script
# accepted only the manually loaded Chromium build, so a wallet installed from
# the Chrome Web Store was refused even though Hive itself trusted it.
$CELLSTrustedExtensionIds = @(
    'habkkkednignfkoffhpbjahcjbikkahh' # manually loaded Chromium build (pinned key)
    'homapjeinjlbdjhhdegcbnldkpkodepo' # Chrome Web Store listing
    'nmmhneekhgaegpmbnhiacglhoncicflc' # interim CRX
)

# An explicitly supplied ID is ADDED to the trusted set rather than replacing
# it, so registering a development build cannot silently disconnect the
# shipped ones.
if ($ExtensionId) {
    $CELLSTrustedExtensionIds += $ExtensionId
}
$CELLSTrustedExtensionIds = @($CELLSTrustedExtensionIds | Select-Object -Unique)
foreach ($id in $CELLSTrustedExtensionIds) {
    if ($id -notmatch $extensionIdPattern) {
        throw 'ExtensionId must be the 32-character Chrome or Edge extension ID.'
    }
}
if ($FirefoxExtensionId -notmatch $firefoxExtensionIdPattern) {
    throw 'FirefoxExtensionId contains unsupported characters.'
}
if (-not $HostPath) {
    $HostPath = Join-Path $PSScriptRoot '..\..\native\CELLS-hive-wallet-host.exe'
}
$HostPath = (Resolve-Path -LiteralPath $HostPath).Path
$installDir = Join-Path $env:LOCALAPPDATA 'CELLS\HiveWalletBridge'
New-Item -ItemType Directory -Force -Path $installDir | Out-Null
$chromiumManifestPath = Join-Path $installDir 'tech.CELLS.hive_wallet.json'
$firefoxManifestPath = Join-Path $installDir 'tech.CELLS.hive_wallet.firefox.json'
$chromiumManifest = [ordered]@{
    name = 'tech.CELLS.hive_wallet'
    description = 'CELLS Hive Wallet native bridge'
    path = $HostPath
    type = 'stdio'
    allowed_origins = @($CELLSTrustedExtensionIds | ForEach-Object { "chrome-extension://$_/" })
}
$firefoxManifest = [ordered]@{
    name = 'tech.CELLS.hive_wallet'
    description = 'CELLS Hive Wallet native bridge'
    path = $HostPath
    type = 'stdio'
    allowed_extensions = @($FirefoxExtensionId)
}
$utf8WithoutBom = New-Object System.Text.UTF8Encoding($false)
[System.IO.File]::WriteAllText($chromiumManifestPath, ($chromiumManifest | ConvertTo-Json -Depth 4), $utf8WithoutBom)
[System.IO.File]::WriteAllText($firefoxManifestPath, ($firefoxManifest | ConvertTo-Json -Depth 4), $utf8WithoutBom)

$registryTargets = @(
    @('HKCU:\Software\Google\Chrome\NativeMessagingHosts\tech.CELLS.hive_wallet', $chromiumManifestPath),
    @('HKCU:\Software\Microsoft\Edge\NativeMessagingHosts\tech.CELLS.hive_wallet', $chromiumManifestPath),
    @('HKCU:\Software\Mozilla\NativeMessagingHosts\tech.CELLS.hive_wallet', $firefoxManifestPath)
)
foreach ($target in $registryTargets) {
    $registryPath = $target[0]
    New-Item -Force -Path $registryPath | Out-Null
    Set-Item -LiteralPath $registryPath -Value $target[1]
}
Write-Host "CELLS Wallet bridge registered for Chromium $($CELLSTrustedExtensionIds -join ', ') and Firefox $FirefoxExtensionId"
