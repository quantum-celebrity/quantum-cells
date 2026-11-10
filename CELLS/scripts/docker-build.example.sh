#!/usr/bin/env bash
# Build the CELLS image (CGO + liboqs in Dockerfile). Run from anywhere.
# Usage: bash CELLS/scripts/docker-build.example.sh [extra docker build args...]
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
# Context must be last; pass extra docker build flags before it (e.g. --no-cache --build-arg).
exec docker build -t CELLS:latest -f "$ROOT/Dockerfile" "$@" "$ROOT"
