#!/usr/bin/env sh
# Set env vars so docker-compose validators POST proof bundles to a local CELLS API.
# Usage: ./scripts/wire-CELLS.sh <api_port> <ngc_ingest_secret> [proof_node_id] [proof_hmac_secret]
# Example: ./scripts/wire-CELLS.sh 8080 "<random-32-byte-secret>" "validator-1" "<separate-random-32-byte-hmac-secret>"
# Then: docker compose up --build  (from apps/CELLS-nvidia-ngc)

API_PORT="${1:-}"
SECRET="${2:-}"
PROOF_NODE="${3:-}"
HMAC_SECRET="${4:-}"

if [ -z "$SECRET" ] || [ -z "$API_PORT" ]; then
	echo "Usage: $0 <api_port> <ngc_ingest_secret> [proof_node_id_optional] [proof_hmac_secret_optional]" >&2
	exit 1
fi

# Export under BOTH the preferred CELLS_* name and the legacy
# CELLSPLUS_* alias so docker-compose containers running mixed sidecar
# versions all see the value (the post-rebrand sidecar reads CELLS_*
# first, the deprecation-window sidecar reads CELLSPLUS_*; setting
# both is cheap and removes a foot-gun). The CELLSplus -> CELLS rebrand
# previously collapsed the CELLSPLUS_* lines into duplicates of the
# CELLS_* lines (incl. an inert self-assignment of CELLS_NGC_REPORT_URL),
# silently breaking any container still on the legacy name.
export CELLS_NGC_INGEST_SECRET="$SECRET"
export CELLS_NGC_REPORT_URL="http://host.docker.internal:${API_PORT}/api/v1/monitoring/ngc-proof"
export CELLSPLUS_NGC_INGEST_SECRET="$SECRET"
export CELLSPLUS_NGC_REPORT_URL="$CELLS_NGC_REPORT_URL"

if [ -n "$PROOF_NODE" ]; then
	export CELLS_NGC_PROOF_NODE_ID="$PROOF_NODE"
	export CELLSPLUS_NGC_PROOF_NODE_ID="$PROOF_NODE"
fi
if [ -n "$HMAC_SECRET" ]; then
	export CELLS_NGC_PROOF_HMAC_SECRET="$HMAC_SECRET"
	export CELLSPLUS_NGC_PROOF_HMAC_SECRET="$HMAC_SECRET"
fi

echo "CELLS_NGC_REPORT_URL=$CELLS_NGC_REPORT_URL"
echo "CELLS_NGC_INGEST_SECRET=(set)"
if [ -n "$PROOF_NODE" ]; then
	echo "CELLS_NGC_PROOF_NODE_ID=$PROOF_NODE"
fi
if [ -n "$HMAC_SECRET" ]; then
	echo "CELLS_NGC_PROOF_HMAC_SECRET=(set)"
fi
echo "Run: docker compose up --build"
echo ""
echo "Ops checklist (better NGC utilization):"
echo "  - Node: set CELLS_NGC_INGEST_SECRET to the same value (enables POST .../ngc-proof)."
echo "  - NVIDIA-lock: align CELLS_NGC_PROOF_NODE_ID with node CELLS_NVIDIA_LOCK_EXPECTED_NODE_ID when binding."
echo "  - If node uses ingest nonce: set CELLS_NGC_FETCH_CHALLENGE=true on sidecar; optional CELLS_NGC_CHALLENGE_JITTER_MAX_SEC for many validators."
echo "  - GPU proofs: use Dockerfile.ngc / validator-gpu so gpu_fingerprint.available=true in bundles."
echo "  - Metrics: CELLS_ngc_proof_ingest_* on /api/metrics/prometheus (dashboard + alerts)."
