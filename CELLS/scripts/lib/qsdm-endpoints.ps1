# Shared public endpoint defaults for local operator scripts.
#
# Keep CELLS.tech as the live product domain, but do not bake one VPS into
# every launcher. A new VPS migration should update config/public-endpoints.json
# or set the matching CELLS_* environment variables, then restart the scripts.

$script:CELLSEndpointDefaults = [ordered]@{
    public_site = "https://CELLS.tech"
    public_api_base = "https://api.CELLS.tech"
    core_api_base = "https://api.CELLS.tech/api/v1"
    home_gateway_relay = "https://api.CELLS.tech"
    home_gateway_slot = "home-validator"
    reference_bootstrap_peer = "/dns4/api.CELLS.tech/tcp/4001/p2p/12D3KooWRH4MGiaRYMZEr9LvdxYrpePT5LPbNqLTMGukD32yhkZ8"
    vps_ssh_target = "root@node.CELLS.tech"
}

function Get-CELLSEndpointConfigPath {
    if (-not [string]::IsNullOrWhiteSpace($env:CELLS_ENDPOINTS_FILE)) {
        return $env:CELLS_ENDPOINTS_FILE
    }
    $scriptsDir = Split-Path -Parent $PSScriptRoot
    $root = Split-Path -Parent $scriptsDir
    return (Join-Path $root "config\public-endpoints.json")
}

function Read-CELLSEndpointConfig {
    $path = Get-CELLSEndpointConfigPath
    if ([string]::IsNullOrWhiteSpace($path) -or -not (Test-Path -LiteralPath $path -PathType Leaf)) {
        return $null
    }
    try {
        return Get-Content -LiteralPath $path -Raw | ConvertFrom-Json
    } catch {
        throw "Invalid CELLS endpoint config at ${path}: $($_.Exception.Message)"
    }
}

function Get-CELLSEndpointValue {
    param(
        [Parameter(Mandatory = $true)][string]$Name,
        [string]$EnvVar = ""
    )

    if (-not [string]::IsNullOrWhiteSpace($EnvVar)) {
        $envValue = [Environment]::GetEnvironmentVariable($EnvVar)
        if (-not [string]::IsNullOrWhiteSpace($envValue)) {
            return $envValue.Trim()
        }
    }

    $config = Read-CELLSEndpointConfig
    if ($null -ne $config) {
        $property = $config.PSObject.Properties[$Name]
        if ($null -ne $property -and -not [string]::IsNullOrWhiteSpace([string]$property.Value)) {
            return ([string]$property.Value).Trim()
        }
    }

    if ($script:CELLSEndpointDefaults.Contains($Name)) {
        return [string]$script:CELLSEndpointDefaults[$Name]
    }
    throw "Unknown CELLS endpoint default: $Name"
}

function Resolve-CELLSEndpointValue {
    param(
        [string]$Value,
        [Parameter(Mandatory = $true)][string]$Name,
        [string]$EnvVar = ""
    )

    if (-not [string]::IsNullOrWhiteSpace($Value)) {
        return $Value.Trim()
    }
    return Get-CELLSEndpointValue -Name $Name -EnvVar $EnvVar
}

function Get-CELLSEndpointHost {
    param([string]$Value)

    if ([string]::IsNullOrWhiteSpace($Value)) {
        return ""
    }
    try {
        return ([Uri]$Value).Host
    } catch {
        return ""
    }
}

function Normalize-CELLSEndpointHost {
    param([string]$Value)

    if ([string]::IsNullOrWhiteSpace($Value)) {
        return ""
    }
    return $Value.Trim().Trim('[', ']').TrimEnd('.').ToLowerInvariant()
}

function Get-CELLSBootstrapPeerHost {
    param([string]$Multiaddr)

    if ([string]::IsNullOrWhiteSpace($Multiaddr)) {
        return ""
    }

    $parts = @($Multiaddr.Trim().Split('/') | Where-Object { -not [string]::IsNullOrWhiteSpace($_) })
    for ($index = 0; $index -lt ($parts.Count - 1); $index += 2) {
        $protocol = $parts[$index].ToLowerInvariant()
        if ($protocol -in @('dns', 'dns4', 'dns6', 'dnsaddr', 'ip4', 'ip6')) {
            return Normalize-CELLSEndpointHost -Value $parts[$index + 1]
        }
    }
    return ""
}

function Get-CELLSSshTargetHost {
    param([string]$Target)

    if ([string]::IsNullOrWhiteSpace($Target)) {
        return ""
    }

    $value = $Target.Trim()
    if ($value -match '^[A-Za-z][A-Za-z0-9+.-]*://') {
        return Normalize-CELLSEndpointHost -Value (Get-CELLSEndpointHost -Value $value)
    }

    $atIndex = $value.LastIndexOf('@')
    if ($atIndex -ge 0) {
        $value = $value.Substring($atIndex + 1)
    }
    if ($value.StartsWith('[')) {
        $closingIndex = $value.IndexOf(']')
        if ($closingIndex -gt 1) {
            return Normalize-CELLSEndpointHost -Value $value.Substring(1, $closingIndex - 1)
        }
    }
    if ($value -match '^(?<host>[^:]+):\d+$') {
        return Normalize-CELLSEndpointHost -Value $Matches.host
    }
    return Normalize-CELLSEndpointHost -Value $value
}

function Get-CELLSReferenceEndpointHosts {
    param(
        [string[]]$CoreApiBases = @(),
        [string[]]$Relays = @(),
        [string[]]$BootstrapPeers = @(),
        [string[]]$SshTargets = @()
    )

    $hosts = @()
    foreach ($value in $CoreApiBases + $Relays) {
        $hostName = Normalize-CELLSEndpointHost -Value (Get-CELLSEndpointHost -Value $value)
        if (-not [string]::IsNullOrWhiteSpace($hostName)) {
            $hosts += $hostName
        }
    }
    foreach ($value in $BootstrapPeers) {
        $hostName = Get-CELLSBootstrapPeerHost -Multiaddr $value
        if (-not [string]::IsNullOrWhiteSpace($hostName)) {
            $hosts += $hostName
        }
    }
    foreach ($value in $SshTargets) {
        $hostName = Get-CELLSSshTargetHost -Target $value
        if (-not [string]::IsNullOrWhiteSpace($hostName)) {
            $hosts += $hostName
        }
    }
    return @($hosts | Sort-Object -Unique)
}

function Test-CELLSNonReferenceEndpointHost {
    param(
        [string]$CandidateHost,
        [string[]]$ReferenceHosts = @()
    )

    $candidate = Normalize-CELLSEndpointHost -Value $CandidateHost
    if ([string]::IsNullOrWhiteSpace($candidate)) {
        return $false
    }
    $normalizedReferences = @($ReferenceHosts | ForEach-Object {
        Normalize-CELLSEndpointHost -Value $_
    } | Where-Object { -not [string]::IsNullOrWhiteSpace($_) })
    return ($normalizedReferences -notcontains $candidate)
}

function Get-CELLSNoProxyList {
    param([string[]]$EndpointValues = @())

    $hosts = @("127.0.0.1", "localhost")
    foreach ($value in $EndpointValues) {
        $hostName = Get-CELLSEndpointHost -Value $value
        if (-not [string]::IsNullOrWhiteSpace($hostName)) {
            $hosts += $hostName
        }
    }
    return (($hosts | Select-Object -Unique) -join ',')
}