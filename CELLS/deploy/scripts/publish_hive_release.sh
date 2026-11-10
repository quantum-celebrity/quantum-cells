#!/usr/bin/env bash
set -euo pipefail

if [[ $# -lt 3 || $# -gt 4 ]]; then
  echo "usage: $0 <stage-dir> <hive-version> <agent-version> [webroot]" >&2
  exit 64
fi

stage_dir="$(cd "$1" && pwd)"
hive_version="$2"
agent_version="$3"
webroot="${4:-/var/www/CELLS}"
# Hive 1.4.21+ trusts only the v2 release key and reads its release from the
# self-contained hive-v2 channel. The v1 files directly under /downloads are
# left untouched for Hive <= 1.4.20.
release_channel="hive-v2"
release_key_id="4081bf2c4755f4c5c1565b4fac75e14a7e0b52042ac3d34bf8a7f525866c64a9"
downloads="$webroot/downloads/$release_channel"
public_downloads="https://CELLS.tech/downloads/$release_channel"

# Signed envelopes and manifests are written by PowerShell ConvertTo-Json,
# which may put more than one space after a colon. Match a JSON string field
# exactly, whatever the whitespace. Reads the JSON text from stdin.
json_field_is() {
  local field="$1"
  local escaped
  escaped="$(printf '%s' "$2" | sed 's/[][\.*^$+?(){}|]/\\&/g')"
  grep -Eq "\"${field}\":[[:space:]]*\"${escaped}\""
}
wallet_extension_version="${CELLS_HIVE_WALLET_EXTENSION_VERSION:-0.5.1}"
wallet_extension="CELLS-hive-wallet-extension-${wallet_extension_version}.zip"
wallet_extension_crx="CELLS-hive-wallet-extension-${wallet_extension_version}.crx"
wallet_extension_chromium="CELLS-hive-wallet-extension-${wallet_extension_version}-chromium.zip"
wallet_extension_chrome="CELLS-hive-wallet-extension-${wallet_extension_version}-chrome.zip"
wallet_extension_edge="CELLS-hive-wallet-extension-${wallet_extension_version}-edge.zip"
wallet_extension_brave="CELLS-hive-wallet-extension-${wallet_extension_version}-brave.zip"
wallet_extension_firefox="CELLS-hive-wallet-extension-${wallet_extension_version}-firefox.zip"
wallet_extension_checksums="CELLS-hive-wallet-extension-${wallet_extension_version}-SHA256SUMS.txt"

if [[ ! "$hive_version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  echo "invalid Hive version: $hive_version" >&2
  exit 64
fi
if [[ ! "$agent_version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  echo "invalid Agent version: $agent_version" >&2
  exit 64
fi

immutable_downloads=(
  "CELLS-hive-${hive_version}-win-x64.exe"
  "CELLS-hive-${hive_version}-win-x64.exe.blockmap"
  "CELLS-hive-${hive_version}-linux-x86_64.AppImage"
  "CELLS-hive-${hive_version}-linux-x64.tar.gz"
  "SHA256SUMS-win.txt"
  "CELLS-hive-${hive_version}-linux-SHA256SUMS.txt"
  "$wallet_extension"
  "$wallet_extension_chromium"
  "$wallet_extension_chrome"
  "$wallet_extension_edge"
  "$wallet_extension_brave"
  "$wallet_extension_firefox"
  "$wallet_extension_checksums"
)
# The Agent and Relay utilities are not part of the Hive updater channel and
# stay directly under /downloads.
agent_downloads=(
  "CELLS-edge-agent-${agent_version}-windows-x86_64.zip"
  "CELLS-edge-agent-${agent_version}-linux-x86_64.tar.gz"
  "CELLS-edge-agent-${agent_version}-windows-x86_64.exe"
  "CELLS-edge-agent-${agent_version}-linux-x86_64"
  "CELLS-edge-control-${agent_version}-windows-x86_64.exe"
  "CELLS-edge-control-${agent_version}-linux-x86_64"
  "CELLS-edge-gpu-helper-${agent_version}-windows-x86_64.exe"
  "CELLS-edge-gpu-helper-${agent_version}-linux-x86_64"
  "CELLS-edge-agent-${agent_version}-SHA256SUMS.txt"
)
if [[ -f "$stage_dir/downloads/$wallet_extension_crx" ]]; then
  immutable_downloads+=("$wallet_extension_crx")
fi
update_manifests=(
  latest.yml
  alpha.yml
  beta.yml
  latest-linux.yml
  alpha-linux.yml
  beta-linux.yml
)
signed_release_manifests=(
  CELLS-hive-release-windows-v2.json
  CELLS-hive-release-linux-v2.json
)

for file in "${immutable_downloads[@]}" "${agent_downloads[@]}" \
  "${update_manifests[@]}" "${signed_release_manifests[@]}"; do
  test -f "$stage_dir/downloads/$file"
done
for file in index.html download.html docs/index.html docs/docs.js; do
  test -f "$stage_dir/$file"
done

(
  cd "$stage_dir/downloads"
  sha256sum -c SHA256SUMS-win.txt
  sha256sum -c "CELLS-hive-${hive_version}-linux-SHA256SUMS.txt"
  sha256sum -c "$wallet_extension_checksums"
  sha256sum -c "CELLS-edge-agent-${agent_version}-SHA256SUMS.txt"
)

for manifest in "${update_manifests[@]}"; do
  grep -qx "version: ${hive_version}" "$stage_dir/downloads/$manifest"
done
windows_payload="$(sed -n 's/.*"manifest_base64":[[:space:]]*"\([^"]*\)".*/\1/p' \
  "$stage_dir/downloads/CELLS-hive-release-windows-v2.json")"
test -n "$windows_payload"
windows_manifest_json="$(printf '%s' "$windows_payload" | base64 --decode)"
json_field_is name "${wallet_extension}" <<<"$windows_manifest_json"
if [[ -f "$stage_dir/downloads/$wallet_extension_crx" ]]; then
  json_field_is name "${wallet_extension_crx}" <<<"$windows_manifest_json"
fi
json_field_is name "${wallet_extension_chromium}" <<<"$windows_manifest_json"
json_field_is name "${wallet_extension_chrome}" <<<"$windows_manifest_json"
json_field_is name "${wallet_extension_edge}" <<<"$windows_manifest_json"
json_field_is name "${wallet_extension_brave}" <<<"$windows_manifest_json"
json_field_is name "${wallet_extension_firefox}" <<<"$windows_manifest_json"
for manifest in "${signed_release_manifests[@]}"; do
  grep -Eq '"schema":[[:space:]]*"CELLS\.signed-release\.v1"' "$stage_dir/downloads/$manifest"
  json_field_is key_id "$release_key_id" \
    <"$stage_dir/downloads/$manifest"
  manifest_payload="$(sed -n 's/.*"manifest_base64":[[:space:]]*"\([^"]*\)".*/\1/p' \
    "$stage_dir/downloads/$manifest")"
  test -n "$manifest_payload"
  manifest_json="$(printf '%s' "$manifest_payload" | base64 --decode)"
  json_field_is version "${hive_version}" <<<"$manifest_json"
done

install -d -o caddy -g caddy -m 0755 "$webroot" "$webroot/downloads" "$downloads" "$webroot/docs"

atomic_install() {
  local source="$1"
  local destination="$2"
  local mode="${3:-0644}"
  local temporary="${destination}.new.$$"
  install -o caddy -g caddy -m "$mode" "$source" "$temporary"
  mv -f "$temporary" "$destination"
}

# Packages go live before clients or pages are told that the release exists.
for file in "${immutable_downloads[@]}"; do
  atomic_install "$stage_dir/downloads/$file" "$downloads/$file"
done
for file in "${agent_downloads[@]}"; do
  atomic_install "$stage_dir/downloads/$file" "$webroot/downloads/$file"
done

for file in \
  "CELLS-hive-${hive_version}-win-x64.exe" \
  "CELLS-hive-${hive_version}-linux-x86_64.AppImage" \
  "CELLS-hive-${hive_version}-linux-x64.tar.gz" \
  "$wallet_extension" \
  "$wallet_extension_chromium" \
  "$wallet_extension_chrome" \
  "$wallet_extension_edge" \
  "$wallet_extension_brave" \
  "$wallet_extension_firefox" \
  "$wallet_extension_checksums"; do
  curl --fail --silent --show-error --head --max-time 30 \
    "$public_downloads/$file" >/dev/null
done
for file in \
  "CELLS-edge-agent-${agent_version}-windows-x86_64.zip" \
  "CELLS-edge-agent-${agent_version}-linux-x86_64.tar.gz"; do
  curl --fail --silent --show-error --head --max-time 30 \
    "https://CELLS.tech/downloads/$file" >/dev/null
done
if [[ -f "$stage_dir/downloads/$wallet_extension_crx" ]]; then
  curl --fail --silent --show-error --head --max-time 30 \
    "$public_downloads/$wallet_extension_crx" >/dev/null
fi

atomic_install "$stage_dir/index.html" "$webroot/index.html"
atomic_install "$stage_dir/download.html" "$webroot/download.html"
atomic_install "$stage_dir/docs/index.html" "$webroot/docs/index.html"
atomic_install "$stage_dir/docs/docs.js" "$webroot/docs/docs.js"

# Exact-version clients see the new release only after every package is public.
for file in "${update_manifests[@]}"; do
  atomic_install "$stage_dir/downloads/$file" "$downloads/$file"
done
for file in "${signed_release_manifests[@]}"; do
  atomic_install "$stage_dir/downloads/$file" "$downloads/$file"
done

public_latest="$(curl --fail --silent --show-error --max-time 30 \
  "$public_downloads/latest.yml")"
grep -qx "version: ${hive_version}" <<<"$public_latest"
for manifest in "${signed_release_manifests[@]}"; do
  public_envelope="$(curl --fail --silent --show-error --max-time 30 \
    "$public_downloads/$manifest")"
  grep -Eq '"schema":[[:space:]]*"CELLS\.signed-release\.v1"' <<<"$public_envelope"
done
public_download_page="$(curl --fail --silent --show-error --max-time 30 \
  "https://CELLS.tech/download.html")"
grep -q "Agent and Relay utilities" <<<"$public_download_page"

echo "Published CELLS Hive ${hive_version} with Agent ${agent_version}."
