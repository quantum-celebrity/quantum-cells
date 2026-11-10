#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'USAGE'
Usage: sudo install_account_service.sh BINARY CONFIG

Installs the CELLS-account binary, its hardened systemd unit, and a completed
private environment file. The script refuses example placeholders.
USAGE
}

if [[ ${EUID:-$(id -u)} -ne 0 ]]; then
  echo "Run this installer as root." >&2
  exit 1
fi
if [[ $# -ne 2 ]]; then
  usage >&2
  exit 2
fi

binary=$(realpath "$1")
config=$(realpath "$2")
script_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
unit=$(realpath "$script_dir/../systemd/CELLS-account.service")
verifier=$(realpath "$script_dir/verify_account_service.sh")
proxy_installer=$(realpath "$script_dir/install_account_proxy_route.sh")
caddy_merger=$(realpath "$script_dir/merge_account_caddy_route.py")

for path in "$binary" "$config" "$unit" "$verifier" "$proxy_installer" "$caddy_merger"; do
  if [[ ! -f "$path" ]]; then
    echo "Required file is missing: $path" >&2
    exit 1
  fi
done
for command_name in systemctl systemd-run; do
  command -v "$command_name" >/dev/null 2>&1 || {
    echo "Required command is missing: $command_name" >&2
    exit 1
  }
done
if grep -Eiq 'REPLACE_WITH|smtp\.example\.|@example\.|\.invalid([[:space:]":]|$)' "$config"; then
  echo "Account configuration still contains example placeholders." >&2
  exit 1
fi
if ! grep -Eq '^CELLS_ACCOUNT_DATA_KEY=.+' "$config"; then
  echo "CELLS_ACCOUNT_DATA_KEY is missing from the account configuration." >&2
  exit 1
fi
if ! grep -Eq '^CELLS_ACCOUNT_(SMTP_HOST|TELEGRAM_CLIENT_ID)=.+' "$config"; then
  echo "Configure SMTP or Telegram before installing CELLS-account." >&2
  exit 1
fi
if ! id CELLS >/dev/null 2>&1; then
  echo "The CELLS service user does not exist. Install CELLS Core first." >&2
  exit 1
fi

# Validate the new binary and configuration before replacing a healthy
# installation. systemd reads EnvironmentFile using the same rules as the
# final service, so the installer never sources or prints secret values.
if ! systemd-run --quiet --wait --pipe --collect \
  --property="EnvironmentFile=$config" \
  "$binary" --check-config; then
  echo "Account configuration preflight failed; the installed service was not changed." >&2
  exit 1
fi

install -d -m0755 -o root -g root /opt/CELLS /etc/CELLS
install -m0755 -o root -g root "$binary" /opt/CELLS/CELLS-account
install -m0755 -o root -g root "$verifier" /opt/CELLS/verify-account-service
install -m0755 -o root -g root "$proxy_installer" /opt/CELLS/install-account-proxy-route
install -m0755 -o root -g root "$caddy_merger" /opt/CELLS/merge-account-caddy-route.py
install -m0600 -o root -g root "$config" /etc/CELLS/account.conf
install -m0644 -o root -g root "$unit" /etc/systemd/system/CELLS-account.service

systemctl daemon-reload
systemctl enable --now CELLS-account.service
sleep 1
/opt/CELLS/verify-account-service --local-only
echo "CELLS-account is installed and healthy."
