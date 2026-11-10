# Example: set env vars so docker-compose validators POST proof bundles to a local CELLS API.
# 1) Start the node with the same secret:  $env:CELLS_NGC_INGEST_SECRET = "<random-32-byte-secret>"
# 2) From this directory:  .\scripts\wire-CELLS.ps1 -ApiPort 8080 -Secret "<random-32-byte-secret>"
#    Optional node binding (must match node's CELLS_NVIDIA_LOCK_EXPECTED_NODE_ID):
#    .\scripts\wire-CELLS.ps1 -ApiPort 8080 -Secret "..." -ProofNodeId "validator-1"
# 3)  docker compose up --build

param(
    [int]$ApiPort = 8080,
    [string]$Secret = "",
    [string]$ProofNodeId = "",
    [string]$ProofHMACSecret = ""
)

if ([string]::IsNullOrWhiteSpace($Secret)) {
    Write-Host "Usage: .\scripts\wire-CELLS.ps1 -ApiPort 8080 -Secret (same as node) [-ProofNodeId id] [-ProofHMACSecret s]" -ForegroundColor Yellow
    exit 1
}

# Export under BOTH the preferred CELLS_* name and the legacy
# CELLSPLUS_* alias so docker-compose containers running mixed sidecar
# versions all see the value (the post-rebrand sidecar reads CELLS_*
# first, the deprecation-window sidecar reads CELLSPLUS_*; setting
# both is cheap and removes a foot-gun). The CELLSplus -> CELLS rebrand
# previously collapsed the CELLSPLUS_* lines into duplicates of the
# CELLS_* lines (incl. an inert self-assignment of CELLS_NGC_REPORT_URL),
# silently breaking any container still on the legacy name.
$env:CELLS_NGC_INGEST_SECRET = $Secret
$env:CELLS_NGC_REPORT_URL = "http://host.docker.internal:$ApiPort/api/v1/monitoring/ngc-proof"
$env:CELLSPLUS_NGC_INGEST_SECRET = $Secret
$env:CELLSPLUS_NGC_REPORT_URL = $env:CELLS_NGC_REPORT_URL
if (![string]::IsNullOrWhiteSpace($ProofNodeId)) {
    $env:CELLS_NGC_PROOF_NODE_ID = $ProofNodeId
    $env:CELLSPLUS_NGC_PROOF_NODE_ID = $ProofNodeId
}
if (![string]::IsNullOrWhiteSpace($ProofHMACSecret)) {
    $env:CELLS_NGC_PROOF_HMAC_SECRET = $ProofHMACSecret
    $env:CELLSPLUS_NGC_PROOF_HMAC_SECRET = $ProofHMACSecret
}
Write-Host "CELLS_NGC_REPORT_URL=$($env:CELLS_NGC_REPORT_URL)" -ForegroundColor Green
Write-Host "CELLS_NGC_INGEST_SECRET=(set)" -ForegroundColor Green
if (![string]::IsNullOrWhiteSpace($ProofNodeId)) {
    Write-Host "CELLS_NGC_PROOF_NODE_ID=$ProofNodeId" -ForegroundColor Green
}
if (![string]::IsNullOrWhiteSpace($ProofHMACSecret)) {
    Write-Host "CELLS_NGC_PROOF_HMAC_SECRET=(set)" -ForegroundColor Green
}
Write-Host "Run: docker compose up --build" -ForegroundColor Cyan
Write-Host ""
Write-Host "Ops checklist (better NGC utilization):" -ForegroundColor DarkGray
Write-Host "  - Node: set CELLS_NGC_INGEST_SECRET to the same value (enables POST .../ngc-proof)." -ForegroundColor DarkGray
Write-Host "  - NVIDIA-lock: align CELLS_NGC_PROOF_NODE_ID with node CELLS_NVIDIA_LOCK_EXPECTED_NODE_ID when binding." -ForegroundColor DarkGray
Write-Host "  - If node uses ingest nonce: set CELLS_NGC_FETCH_CHALLENGE=true on sidecar; optional CELLS_NGC_CHALLENGE_JITTER_MAX_SEC for many validators." -ForegroundColor DarkGray
Write-Host "  - GPU proofs: use Dockerfile.ngc / validator-gpu so gpu_fingerprint.available=true in bundles." -ForegroundColor DarkGray
Write-Host "  - Metrics: CELLS_ngc_proof_ingest_* on /api/metrics/prometheus (dashboard + alerts)." -ForegroundColor DarkGray
