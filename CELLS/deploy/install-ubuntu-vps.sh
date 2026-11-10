#!/bin/bash
# One-time install: CELLS node on Ubuntu 24.04+ (as root)
# See docs/docs/UBUNTU_DEPLOYMENT.md. Run: bash install-ubuntu-vps.sh
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)"
# shellcheck source=scripts/authorized-block-producers.sh
source "${SCRIPT_DIR}/scripts/authorized-block-producers.sh"

CELLS_HOME="${CELLS_HOME:-$HOME/CELLS}"
CELLS_GIT="${CELLS_GIT:-https://github.com/blackbeardONE/CELLS.git}"
GO_TGZ="${GO_TGZ:-https://go.dev/dl/go1.23.4.linux-amd64.tar.gz}"
# Install directory keeps the legacy 'CELLS' name during the rebrand
# migration window so systemd units and operator bookmarks remain valid.
# See CELLS/docs/docs/REBRAND_NOTES.md.
INSTALL_DIR="/opt/CELLS"

if [[ "$(id -u)" -ne 0 ]]; then
  echo "Run as root (e.g. sudo bash $0)"
  exit 1
fi

SIGNED_VOTES_RAW="${CELLS_REQUIRE_SIGNED_VOTES:-false}"
case "$(printf '%s' "$SIGNED_VOTES_RAW" | tr '[:upper:]' '[:lower:]')" in
  1|true|yes|on) CELLS_REQUIRE_SIGNED_VOTES_VALUE=true ;;
  0|false|no|off|"") CELLS_REQUIRE_SIGNED_VOTES_VALUE=false ;;
  *)
    echo "Invalid CELLS_REQUIRE_SIGNED_VOTES=${SIGNED_VOTES_RAW}; use true or false" >&2
    exit 1
    ;;
esac

CELLS_SIGNED_MESSAGE_ACTIVATION_HEIGHT_VALUE="${CELLS_SIGNED_MESSAGE_ACTIVATION_HEIGHT:-0}"
if ! [[ "$CELLS_SIGNED_MESSAGE_ACTIVATION_HEIGHT_VALUE" =~ ^[0-9]+$ ]]; then
  echo "Invalid CELLS_SIGNED_MESSAGE_ACTIVATION_HEIGHT=${CELLS_SIGNED_MESSAGE_ACTIVATION_HEIGHT_VALUE}; use an integer block height" >&2
  exit 1
fi
CELLS_TASK_ACTION_SIGNATURE_ACTIVATION_HEIGHT_VALUE="${CELLS_TASK_ACTION_SIGNATURE_ACTIVATION_HEIGHT:-625000}"
if ! [[ "$CELLS_TASK_ACTION_SIGNATURE_ACTIVATION_HEIGHT_VALUE" =~ ^[0-9]+$ ]]; then
  echo "Invalid CELLS_TASK_ACTION_SIGNATURE_ACTIVATION_HEIGHT=${CELLS_TASK_ACTION_SIGNATURE_ACTIVATION_HEIGHT_VALUE}; use an integer block height" >&2
  exit 1
fi
CELLS_TX_CONTENT_ROOT_ACTIVATION_HEIGHT_VALUE="${CELLS_TX_CONTENT_ROOT_ACTIVATION_HEIGHT:-625000}"
if ! [[ "$CELLS_TX_CONTENT_ROOT_ACTIVATION_HEIGHT_VALUE" =~ ^[0-9]+$ ]]; then
  echo "Invalid CELLS_TX_CONTENT_ROOT_ACTIVATION_HEIGHT=${CELLS_TX_CONTENT_ROOT_ACTIVATION_HEIGHT_VALUE}; use an integer block height" >&2
  exit 1
fi
CELLS_ENROLLMENT_STATE_ROOT_ACTIVATION_HEIGHT_VALUE="${CELLS_ENROLLMENT_STATE_ROOT_ACTIVATION_HEIGHT:-625000}"
if ! [[ "$CELLS_ENROLLMENT_STATE_ROOT_ACTIVATION_HEIGHT_VALUE" =~ ^[0-9]+$ ]]; then
  echo "Invalid CELLS_ENROLLMENT_STATE_ROOT_ACTIVATION_HEIGHT=${CELLS_ENROLLMENT_STATE_ROOT_ACTIVATION_HEIGHT_VALUE}; use an integer block height" >&2
  exit 1
fi
if [[ "$CELLS_REQUIRE_SIGNED_VOTES_VALUE" == "true" && "$CELLS_SIGNED_MESSAGE_ACTIVATION_HEIGHT_VALUE" == "0" ]]; then
  echo "CELLS_REQUIRE_SIGNED_VOTES=true requires CELLS_SIGNED_MESSAGE_ACTIVATION_HEIGHT to be a shared future height" >&2
  exit 1
fi
if [[ "$CELLS_REQUIRE_SIGNED_VOTES_VALUE" == "false" && "$CELLS_SIGNED_MESSAGE_ACTIVATION_HEIGHT_VALUE" != "0" ]]; then
  echo "CELLS_SIGNED_MESSAGE_ACTIVATION_HEIGHT must be 0 while CELLS_REQUIRE_SIGNED_VOTES=false" >&2
  exit 1
fi

CELLS_STRICT_SECRETS_VALUE="$(CELLS_normalize_bool "CELLS_STRICT_SECRETS" "${CELLS_STRICT_SECRETS:-false}")"
CELLS_AUTHORIZED_BLOCK_PRODUCERS_VALUE="${CELLS_AUTHORIZED_BLOCK_PRODUCERS:-}"
AUTHORIZED_BLOCK_PRODUCERS_TOML="$(CELLS_render_authorized_block_producers_toml "$CELLS_AUTHORIZED_BLOCK_PRODUCERS_VALUE")"
AUTHORIZED_BLOCK_PRODUCER_COUNT="$(CELLS_authorized_block_producer_count "$CELLS_AUTHORIZED_BLOCK_PRODUCERS_VALUE")"
if [[ "$CELLS_STRICT_SECRETS_VALUE" == "true" && "$AUTHORIZED_BLOCK_PRODUCER_COUNT" == "0" ]]; then
  echo "CELLS_STRICT_SECRETS=true requires CELLS_AUTHORIZED_BLOCK_PRODUCERS to pin at least one producer ID" >&2
  exit 1
fi

export DEBIAN_FRONTEND=noninteractive
apt-get update -y
apt-get upgrade -y
apt-get install -y --no-install-recommends \
  build-essential cmake git curl wget ufw htop \
  ca-certificates pkg-config libssl-dev libsqlite3-dev

# Go toolchain: use /usr/local/go (1.23.x) for reproducible builds
if [[ ! -x /usr/local/go/bin/go ]]; then
  echo "=== Installing Go from ${GO_TGZ} ==="
  TMP="$(mktemp)"
  wget -qO "$TMP" "$GO_TGZ"
  rm -rf /usr/local/go
  tar -C /usr/local -xzf "$TMP"
  rm -f "$TMP"
  grep -q '/usr/local/go/bin' /root/.profile 2>/dev/null || echo 'export PATH=$PATH:/usr/local/go/bin' >> /root/.profile
fi
export PATH="/usr/local/go/bin:${PATH:-}"
go version

# Use pre-uploaded source if present; otherwise clone from git
if [[ -f "$CELLS_HOME/scripts/rebuild_liboqs.sh" && -d "$CELLS_HOME/source" ]]; then
  echo "=== Using existing CELLS tree at $CELLS_HOME (no git clone) ==="
elif [[ ! -d "$CELLS_HOME/.git" ]]; then
  echo "=== Cloning CELLS ledger ==="
  git clone --depth 1 "$CELLS_GIT" "$CELLS_HOME"
else
  echo "=== Updating existing clone ==="
  git -C "$CELLS_HOME" pull --ff-only || true
fi

cd "$CELLS_HOME"
chmod +x scripts/rebuild_liboqs.sh scripts/build.sh 2>/dev/null || true

echo "=== Building liboqs (long; 10–30+ min on small VPS) ==="
./scripts/rebuild_liboqs.sh

echo "=== Building CELLS ==="
./scripts/build.sh
test -f ./CELLS

if ! getent passwd CELLS &>/dev/null; then
  useradd -r -s /bin/false -d "$INSTALL_DIR" CELLS
fi
mkdir -p "$INSTALL_DIR"
chown -R CELLS:CELLS "$INSTALL_DIR"

install -m 0755 ./CELLS "$INSTALL_DIR/CELLS"
if [[ -d ./liboqs_install ]]; then
  cp -a ./liboqs_install "$INSTALL_DIR/"
  chown -R CELLS:CELLS "$INSTALL_DIR/liboqs_install"
fi

# Production config: HTTP API without TLS to avoid cert requirement; paths under /opt
cat > /tmp/CELLS.production.toml <<EOF
[network]
port = 4001
bootstrap_peers = []

[storage]
type = "sqlite"
sqlite_path = "/opt/CELLS/CELLS.db"

[monitoring]
dashboard_port = 8081
log_viewer_port = 8080
log_file = "/opt/CELLS/CELLS.log"
log_level = "INFO"

[api]
port = 8443
enable_tls = false
tls_cert_file = ""
tls_key_file = ""
strict_secrets = ${CELLS_STRICT_SECRETS_VALUE}

[wallet]
initial_balance = 1000.0

[governance]
proposal_file = "/opt/CELLS/proposals.json"

[consensus]
# Consensus messages are signed by every modern validator. Enforce rejection of
# unsigned messages only at one operator-approved future height shared by the
# entire validator set. Set CELLS_REQUIRE_SIGNED_VOTES=true and
# CELLS_SIGNED_MESSAGE_ACTIVATION_HEIGHT=<height> before running this installer.
require_signed_votes = ${CELLS_REQUIRE_SIGNED_VOTES_VALUE}
signed_message_activation_height = ${CELLS_SIGNED_MESSAGE_ACTIVATION_HEIGHT_VALUE}
task_action_signature_activation_height = ${CELLS_TASK_ACTION_SIGNATURE_ACTIVATION_HEIGHT_VALUE}
tx_content_root_activation_height = ${CELLS_TX_CONTENT_ROOT_ACTIVATION_HEIGHT_VALUE}
enrollment_state_root_activation_height = ${CELLS_ENROLLMENT_STATE_ROOT_ACTIVATION_HEIGHT_VALUE}
signer_key_path = "CELLS_consensus_signer.json"
${AUTHORIZED_BLOCK_PRODUCERS_TOML}

[performance]
# Demo/auto-txgen cadence; production should be long or a real client
transaction_interval = "1h"
health_check_interval = "30s"
EOF
install -m 0644 /tmp/CELLS.production.toml "$INSTALL_DIR/CELLS.toml"
rm -f /tmp/CELLS.production.toml
chown CELLS:CELLS "$INSTALL_DIR/CELLS.toml"
touch "$INSTALL_DIR/proposals.json" 2>/dev/null || true
chown CELLS:CELLS "$INSTALL_DIR/proposals.json" 2>/dev/null || true

if [[ -f config/CELLS.service ]]; then
  install -m 0644 config/CELLS.service /etc/systemd/system/CELLS.service
else
  echo "Missing config/CELLS.service in repo" >&2
  exit 1
fi
systemctl daemon-reload
systemctl enable CELLS
systemctl restart CELLS

echo "=== Firewall (ufw) ==="
ufw allow 22/tcp
ufw allow 4001/tcp
ufw allow 8080/tcp
ufw allow 8081/tcp
ufw allow 8443/tcp
ufw --force enable || true

echo ""
echo "=== Done ==="
systemctl --no-pager -l status CELLS || true
echo "Dashboard: http://$(curl -s ifconfig.me 2>/dev/null || echo YOUR_IP):8081"
echo "Logs: journalctl -u CELLS -f"
