#!/usr/bin/env bash
# Local parity with .github/workflows/CELLS-go.yml (build-test + govulncheck)
# and validate-deploy.yml (compose + kubectl dry-run). Run from monorepo root (parent of CELLS/).
# Usage: bash CELLS/scripts/ci-local-parity.sh
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
CELLS_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
REPO_ROOT="$(cd "$CELLS_ROOT/.." && pwd)"

echo "==> Repo root: $REPO_ROOT"
echo "==> CELLS root: $CELLS_ROOT"

if command -v docker >/dev/null 2>&1; then
	echo "==> docker compose config (cluster)"
	docker compose -f "$REPO_ROOT/CELLS/deploy/docker-compose.cluster.yml" config -q
	echo "==> docker compose config (single)"
	docker compose -f "$REPO_ROOT/CELLS/deploy/docker-compose.single.yml" config -q
else
	echo "SKIP: docker not in PATH (install Docker for compose validation)"
fi

echo "==> go build (no CGO)"
bash "$CELLS_ROOT/scripts/go-build-no-cgo.sh" "/tmp/CELLS-ci-local"

echo "==> go test -short (no CGO)"
bash "$CELLS_ROOT/scripts/go-test-short-no-cgo.sh"

if [ "${CI_LOCAL_PARITY_CGO_MIGRATE:-}" = "1" ]; then
	echo "==> go test ./cmd/migrate (CGO + liboqs — requires CELLS/liboqs_install; CI_LOCAL_PARITY_CGO_MIGRATE=1)"
	bash "$CELLS_ROOT/scripts/go-test-migrate-cgo.sh"
fi

echo "NOTE: Kubernetes manifest dry-run runs in CI (validate-deploy.yml); local kubectl often needs a cluster context (skip here)."
echo "NOTE: Optional migrate CGO tests (needs liboqs): CI_LOCAL_PARITY_CGO_MIGRATE=1 or bash CELLS/scripts/go-test-migrate-cgo.sh"

if [ "${SKIP_GOVULNCHECK:-}" = "1" ]; then
	echo "SKIP: govulncheck (SKIP_GOVULNCHECK=1)"
else
	echo "==> govulncheck (set SKIP_GOVULNCHECK=1 to skip, e.g. known transitive advisories)"
	cd "$CELLS_ROOT/source"
	export CELLS_METRICS_REGISTER_STRICT=1
	unset CGO_CFLAGS CGO_LDFLAGS 2>/dev/null || true
	export CGO_ENABLED=0
	bash "$CELLS_ROOT/scripts/govulncheck-filter.sh"
fi

echo "OK: ci-local-parity finished"
