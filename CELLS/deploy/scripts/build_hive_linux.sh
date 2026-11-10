#!/usr/bin/env bash
set -euo pipefail

if [[ $# -lt 2 ]]; then
  echo "usage: $0 <hive-source-dir> <output-dir>" >&2
  exit 64
fi

source_dir="$(cd "$1" && pwd)"
output_dir="$2"
CELLS_source_dir="${CELLS_SOURCE_DIR:-$source_dir/../../../CELLS/source}"
native_dir="$source_dir/native/linux/x64"
edge_agent="$native_dir/CELLS-edge-agent"
edge_control="$native_dir/CELLS-edge-control"
gpu_helper="$native_dir/CELLS-edge-gpu-helper"
miner="$native_dir/CELLSminer-console"
cuda_solver="$native_dir/CELLS-miner-cuda-solver"
wallet_host="$native_dir/CELLS-hive-wallet-host"

mkdir -p "$output_dir"
mkdir -p "$native_dir"

export CI=1
export CSC_IDENTITY_AUTO_DISCOVERY=false
export PUPPETEER_SKIP_DOWNLOAD=true
export PUPPETEER_SKIP_CHROMIUM_DOWNLOAD=true

cd "$source_dir"

package_version="$(node -p "require('./package.json').version")"
hive_version="$(node -p "require('./release/app/package.json').version")"
if [[ "$package_version" != "$hive_version" ]]; then
  echo "Hive package version $package_version does not match runtime version $hive_version. Update both manifests before packaging." >&2
  exit 65
fi
edge_agent_version="$(tr -d '[:space:]' < "$source_dir/../../CELLS-edge-agent/VERSION")"
miner_git_sha="$(git -C "$CELLS_source_dir" rev-parse --short HEAD 2>/dev/null || echo unknown)"
miner_build_date="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
miner_ldflags="-s -w -X github.com/blackbeardONE/CELLS/pkg/buildinfo.Version=hive-v${hive_version} -X github.com/blackbeardONE/CELLS/pkg/buildinfo.GitSHA=${miner_git_sha} -X github.com/blackbeardONE/CELLS/pkg/buildinfo.BuildDate=${miner_build_date}"

if [[ -n "${CELLS_PREBUILT_CLI:-}" ]]; then
  if [[ ! -f "$CELLS_PREBUILT_CLI" ]]; then
    echo "CELLS_PREBUILT_CLI does not point to a file." >&2
    exit 66
  fi
  if [[ "$(readlink -f "$CELLS_PREBUILT_CLI")" != "$(readlink -f "$native_dir/CELLScli")" ]]; then
    install -m 0755 "$CELLS_PREBUILT_CLI" "$native_dir/CELLScli"
  else
    chmod 0755 "$native_dir/CELLScli"
  fi
elif command -v go >/dev/null 2>&1; then
  CELLS_source_dir="$(cd "$CELLS_source_dir" && pwd)"
  (
    cd "$CELLS_source_dir"
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
      -trimpath \
      -tags dilithium_circl \
      -ldflags="-s -w" \
      -o "$native_dir/CELLScli" \
      ./cmd/CELLScli
  )
  chmod 0755 "$native_dir/CELLScli"
elif [[ -f "$native_dir/CELLScli" ]]; then
  chmod 0755 "$native_dir/CELLScli"
else
  echo "Go is required to build the bundled CELLS signer CLI." >&2
  exit 69
fi

if command -v go >/dev/null 2>&1; then
  CELLS_source_dir="$(cd "$CELLS_source_dir" && pwd)"
  (
    cd "$CELLS_source_dir"
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
      -trimpath \
      -ldflags="-s -w" \
      -o "$wallet_host" \
      ./cmd/CELLS-hive-wallet-host
  )
  chmod 0755 "$wallet_host"
elif [[ -f "$wallet_host" ]]; then
  chmod 0755 "$wallet_host"
else
  echo "The Linux CELLS Hive wallet native host is missing and Go is unavailable." >&2
  exit 69
fi

if [[ -n "${CELLS_PREBUILT_MINER:-}" ]]; then
  if [[ ! -f "$CELLS_PREBUILT_MINER" ]]; then
    echo "CELLS_PREBUILT_MINER does not point to a file." >&2
    exit 66
  fi
  install -m 0755 "$CELLS_PREBUILT_MINER" "$miner"
elif command -v go >/dev/null 2>&1; then
  CELLS_source_dir="$(cd "$CELLS_source_dir" && pwd)"
  (
    cd "$CELLS_source_dir"
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
      -trimpath \
      -ldflags="$miner_ldflags" \
      -o "$miner" \
      ./cmd/CELLSminer-console
  )
  chmod 0755 "$miner"
elif [[ -f "$miner" ]]; then
  chmod 0755 "$miner"
else
  echo "The Linux CELLS console miner is missing and Go is unavailable." >&2
  exit 69
fi

if [[ -n "${CELLS_PREBUILT_CUDA_SOLVER:-}" ]]; then
  if [[ ! -f "$CELLS_PREBUILT_CUDA_SOLVER" ]]; then
    echo "CELLS_PREBUILT_CUDA_SOLVER does not point to a file." >&2
    exit 66
  fi
  install -m 0755 "$CELLS_PREBUILT_CUDA_SOLVER" "$cuda_solver"
elif command -v nvcc >/dev/null 2>&1; then
  cuda_host_compiler="$(command -v g++-12 || command -v g++)"
  nvcc -O3 -std=c++17 -ccbin "$cuda_host_compiler" \
    -gencode arch=compute_75,code=sm_75 \
    -gencode arch=compute_86,code=sm_86 \
    -gencode arch=compute_89,code=sm_89 \
    -gencode arch=compute_90,code=sm_90 \
    "$CELLS_source_dir/cmd/CELLS-miner-cuda-solver/main.cu" \
    -o "$cuda_solver"
  chmod 0755 "$cuda_solver"
elif [[ -f "$cuda_solver" ]]; then
  chmod 0755 "$cuda_solver"
else
  echo "The Linux CELLS CUDA miner solver is missing and nvcc is unavailable." >&2
  exit 69
fi

if [[ -n "${CELLS_PREBUILT_EDGE_AGENT:-}" ]]; then
  if [[ ! -f "$CELLS_PREBUILT_EDGE_AGENT" ]]; then
    echo "CELLS_PREBUILT_EDGE_AGENT does not point to a file." >&2
    exit 66
  fi
  install -m 0755 "$CELLS_PREBUILT_EDGE_AGENT" "$edge_agent"
elif command -v go >/dev/null 2>&1; then
  CELLS_source_dir="$(cd "$CELLS_source_dir" && pwd)"
  (
    cd "$CELLS_source_dir"
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
      -trimpath \
      -ldflags="-s -w -X main.version=${edge_agent_version}" \
      -o "$edge_agent" \
      ./cmd/CELLS-edge-agent
  )
  chmod 0755 "$edge_agent"
elif [[ -f "$edge_agent" ]]; then
  chmod 0755 "$edge_agent"
else
  echo "The Linux CELLS edge agent is missing and Go is unavailable." >&2
  exit 69
fi

if [[ -n "${CELLS_PREBUILT_EDGE_CONTROL:-}" ]]; then
  if [[ ! -f "$CELLS_PREBUILT_EDGE_CONTROL" ]]; then
    echo "CELLS_PREBUILT_EDGE_CONTROL does not point to a file." >&2
    exit 66
  fi
  install -m 0755 "$CELLS_PREBUILT_EDGE_CONTROL" "$edge_control"
elif command -v go >/dev/null 2>&1; then
  CELLS_source_dir="$(cd "$CELLS_source_dir" && pwd)"
  (
    cd "$CELLS_source_dir"
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
      -trimpath \
      -ldflags="-s -w -X main.version=${edge_agent_version}" \
      -o "$edge_control" \
      ./cmd/CELLS-edge-control
  )
  chmod 0755 "$edge_control"
elif [[ -f "$edge_control" ]]; then
  chmod 0755 "$edge_control"
else
  echo "The Linux CELLS Edge Control app is missing and Go is unavailable." >&2
  exit 69
fi

if [[ -n "${CELLS_PREBUILT_GPU_HELPER:-}" ]]; then
  if [[ ! -f "$CELLS_PREBUILT_GPU_HELPER" ]]; then
    echo "CELLS_PREBUILT_GPU_HELPER does not point to a file." >&2
    exit 66
  fi
  install -m 0755 "$CELLS_PREBUILT_GPU_HELPER" "$gpu_helper"
elif command -v nvcc >/dev/null 2>&1; then
  cuda_host_compiler="$(command -v g++-12 || command -v g++)"
  nvcc -O3 -std=c++17 -arch=sm_75 -ccbin "$cuda_host_compiler" \
    "$source_dir/../../../CELLS/source/cmd/CELLS-edge-gpu-helper/main.cu" \
    -o "$gpu_helper"
  chmod 0755 "$gpu_helper"
elif [[ -f "$gpu_helper" ]]; then
  chmod 0755 "$gpu_helper"
else
  echo "The Linux CELLS GPU helper is missing and nvcc is unavailable." >&2
  exit 69
fi

# Install the compiler/tooling and runtime dependency trees independently.
# Scripts run only after both trees exist so electron-rebuild can resolve the
# root development dependencies while rebuilding release/app for Linux.
npm ci --ignore-scripts
npm --prefix release/app ci --ignore-scripts
npm exec electron-builder -- install-app-deps
npm run build
npm exec electron-builder -- \
  --linux AppImage \
  --x64 \
  --publish never \
  "--config.directories.output=$output_dir"

test -x "$output_dir/linux-unpacked/resources/native/CELLScli"
test -x "$output_dir/linux-unpacked/resources/native/CELLS-hive-wallet-host"
test -x "$output_dir/linux-unpacked/resources/edge/CELLS-edge-agent"
test -x "$output_dir/linux-unpacked/resources/edge/CELLS-edge-control"
test -x "$output_dir/linux-unpacked/resources/edge/CELLS-edge-gpu-helper"
test -x "$output_dir/linux-unpacked/resources/miner/CELLSminer-console"
test -x "$output_dir/linux-unpacked/resources/miner/CELLS-miner-cuda-solver"
"$output_dir/linux-unpacked/resources/miner/CELLSminer-console" --version

version="$hive_version"
archive="$output_dir/CELLS-hive-${version}-linux-x64.tar.gz"
tar -C "$output_dir" \
  --transform "s,^linux-unpacked,CELLS-hive-${version}-linux-x64," \
  -czf "$archive" \
  linux-unpacked

appimages=("$output_dir"/CELLS-hive-*-linux-*.AppImage)
archives=("$output_dir"/CELLS-hive-*-linux-*.tar.gz)

if [[ ! -f "${appimages[0]}" || ! -f "${archives[0]}" ]]; then
  echo "Linux release artifacts were not generated as expected." >&2
  exit 1
fi

(
  cd "$output_dir"
  sha256sum CELLS-hive-*-linux-*.AppImage CELLS-hive-*-linux-*.tar.gz \
    > SHA256SUMS-linux.txt
)
