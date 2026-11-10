# Rebrand notes — CELLS+ → CELLS, with the native coin **Cell (CELL)** [COMPLETED]

> **Status: archived.** The rebrand from *CELLS+* / *CELLSplus* to *CELLS* / *CELLS*
> is complete. All legacy identifiers (environment variables, HTTP headers,
> configuration file names, SDK package names, CI workflow names, Prometheus
> metric aliases, directory names, and binary names) have been removed from
> the codebase. The `pkg/envcompat`, `pkg/monitoring/prometheus_prefix_migration`,
> and duplicate SDK-alias surfaces have been retired. The document below is
> retained for historical reference only; no item in it is still load-bearing.

---


> Audience: node operators, SDK consumers, downstream maintainers. This document is
> normative for the in-repo rebrand executed in Phase 1 of `Major Update.md`. It is
> **not** a marketing document — product copy lives on `CELLS.tech` and in the
> landing / dashboard pages.

## 1. Scope

The platform is migrating from the transitional name **CELLS+** back to **CELLS** and
introducing a native coin, **Cell (CELL)**. No on-chain data, address format, wire
protocol, consensus rule, or cryptographic primitive is changed by the rebrand
itself; this is a **naming and identity** migration plus the first ratchet of the
Cell identity (branding constants, tokenomics documents, trust endpoints). Actual
coin emission and mining protocol work lands in later phases (see `CELL_TOKENOMICS.md`
and `MINING_PROTOCOL.md` once those land).

Anything that is **not** changed by this document:

- The Go module path remains `github.com/blackbeardONE/CELLS`.
- Address formats, signature schemes (ML-DSA-87 via liboqs), GossipSub topics, and
  the `/api/v1/*` REST surface remain unchanged.
- Existing databases, config files, and systemd units keep working without renaming.
- Proof-of-Entanglement + BFT consensus semantics are unchanged and remain CPU-only.
- NVIDIA NGC attestation remains **optional** and **not a consensus rule** — it is a
  transparency signal, re-exposed under the `/api/v1/trust/*` endpoints in Phase 5.

## 2. Migration window

| Date (relative) | Milestone |
|---|---|
| T + 0 (Phase 1 landing) | Preferred names accepted everywhere. Legacy names continue to work. A one-shot deprecation warning is logged the first time a legacy env var or HTTP header is observed. |
| T + 6 months | Legacy names still accepted. Documentation stops referring to them except in migration tables. |
| T + 12 months | Legacy env vars / headers / config file names removed from the deprecation shim. SDK legacy class aliases removed in the next major version bump. |
| T + ? (governance decision) | Repository root directory renamed from `CELLS+/` to `CELLS/` and any folder whose literal name contains `CELLSplus` renamed. This is a **one-way door** change that waits on a coordinated release and is not performed silently in a patch release. |

## 3. Deprecation table — symbol / file / protocol

### 3.1 Environment variables

All `CELLSPLUS_*` environment variables have a preferred `CELLS_*` equivalent. The
shim in `pkg/envcompat` reads the preferred name first, falls back to the legacy
name, and logs a one-shot deprecation warning when only the legacy name is present.

| Preferred | Legacy | Notes |
|---|---|---|
| `CELLS_STRICT_PRODUCTION_SECRETS` | `CELLSPLUS_STRICT_PRODUCTION_SECRETS` | Boolean. Enforces strict handling of admin / HMAC secrets in production. |
| `CELLS_NGC_INGEST_SECRET` | `CELLSPLUS_NGC_INGEST_SECRET` | HMAC key for NGC proof ingest. |
| `CELLS_METRICS_SCRAPE_SECRET` | `CELLSPLUS_METRICS_SCRAPE_SECRET` | Shared secret for the internal Prometheus scrape endpoint. |
| `CELLS_PUBLISH_MESH_COMPANION` | `CELLSPLUS_PUBLISH_MESH_COMPANION` | Boolean truthy gate for publishing the mesh companion feed. |
| `CELLS_WASM_PREFLIGHT_MODULE` | `CELLSPLUS_WASM_PREFLIGHT_MODULE` | Path to the WASM preflight module. |
| `CELLS_METRICS_REGISTER_STRICT` | `CELLSPLUS_METRICS_REGISTER_STRICT` | Boolean. Fails startup when Prometheus collector registration conflicts. |

Apply the rule: **if you are setting a `CELLSPLUS_*` variable today, add a `CELLS_*`
variable with the same value. Remove the legacy one after you have confirmed the
node starts clean.** Operators running systemd units can keep both names set during
the migration; when both are present the preferred name wins.

### 3.2 HTTP headers

| Preferred | Legacy | Direction |
|---|---|---|
| `X-CELLS-NGC-Secret` | `X-CELLSPLUS-NGC-Secret` | Client → node, NGC ingest auth. |
| `X-CELLS-Metrics-Scrape-Secret` | `X-CELLSPLUS-Metrics-Scrape-Secret` | Scrape client → dashboard. |

The node accepts either header on ingress. SDKs emit the preferred header.

### 3.3 NGC proof JSON fields

Bundles produced by the NVIDIA NGC sidecar are accepted with either the `CELLS_*`
or `CELLSplus_*` field names. The HMAC payload canonicalizes on the value regardless
of which field name carried it, so an in-flight rename of the sidecar does not
invalidate proofs.

| Preferred field | Legacy field |
|---|---|
| `CELLS_node_id` | `CELLSplus_node_id` |
| `CELLS_proof_hmac` | `CELLSplus_proof_hmac` |
| `CELLS_ingest_nonce` | `CELLSplus_ingest_nonce` |

### 3.4 Configuration files

The node loads the first file it finds in the following order. Each name is
resolved in both the current working directory and, where applicable, the
standard config search path.

| Preferred | Legacy |
|---|---|
| `CELLS.toml` | `CELLSplus.toml` |
| `CELLS.yaml` | `CELLSplus.yaml` |
| `CELLS.json` | `CELLSplus.json` |

Default database and log paths also prefer `CELLS.db` / `CELLS.log` but fall back to
an existing `CELLSplus.db` / `CELLSplus.log` if found, so running nodes do not
start a new chain on upgrade.

### 3.5 SDK symbols

#### Go

The plan below was **not** what shipped. Recorded as written, with the outcome
after it:

> The existing package identifier `CELLSplus` at `sdk/go/` is kept; a new preferred
> package is available at `sdk/go/CELLS/`:
>
> ```go
> // Legacy import (still supported during the deprecation window):
> import CELLSplus "github.com/blackbeardONE/CELLS/sdk/go"
> c := CELLSplus.NewClient("http://node:8080")
>
> // Preferred import:
> import "github.com/blackbeardONE/CELLS/sdk/go/CELLS"
> c := CELLS.NewClient("http://node:8080")
> ```
>
> Type aliases `CELLSClient = Client` and `CELLSPlusClient = Client` are exported from
> both packages so existing code continues to compile.

**What actually shipped (verified 2026-08-16).** There is one Go package, not two:
`sdk/go/CELLS/` was never created. Neither alias exists -- `grep -rn 'CELLSClient\|CELLSPlusClient'
CELLS/source/sdk/go/` returns nothing. The package exports `Client`, constructed
with `NewClient`. `CELLSClient` is a JavaScript class
(`CELLS/source/sdk/javascript/CELLS.js:40`), not a Go alias.

The importable path is also not the one above. The SDK is a nested module inside a
repository named `CELLS`, so the module path doubles:

```go
import CELLS "github.com/blackbeardONE/CELLS/CELLS/source/sdk/go"
c := CELLS.NewClient("http://node:8080")
```

#### JavaScript

The npm package `CELLSplus` is superseded by `CELLS`. Both `CELLS.js` and the legacy
`CELLSplus.js` are shipped in the same tarball during the deprecation window.
`CELLSClient` is the preferred class name; `CELLSPlusClient` is a legacy alias
referring to the same constructor.

```js
const { CELLSClient } = require('CELLS');
const c = new CELLSClient('http://node:8080');
```

### 3.6 CI workflows

| Preferred | Legacy |
|---|---|
| `.github/workflows/CELLS-go.yml` | `.github/workflows/CELLSplus-go.yml` |
| `.github/workflows/CELLS-scylla-staging.yml` | `.github/workflows/CELLSplus-scylla-staging.yml` |

Workflow `name:` and `concurrency.group:` fields were updated accordingly. New
workflows introduced by the Major Update (`release-container.yml` split,
`mining-kernels.yml`, `sdk-js.yml`, `audit-gate.yml`) land in Phase 2 and later
and use the `CELLS-` prefix from the start.

### 3.7 Prometheus metric names

**Status: dual-emit landed.** Every metric the node exposes on
`/api/metrics/prometheus` is now emitted under **both** the legacy
`CELLSplus_*` prefix and the new `CELLS_*` prefix simultaneously. Existing
Grafana dashboards and alert-manager rules referencing `CELLSplus_*`
continue to work unchanged; dashboards written against the new prefix
also work. Per-metric help text on the legacy copies is annotated
`[DEPRECATED alias of CELLS_<name>; set CELLS_METRICS_EMIT_LEGACY=0 to
suppress.]` so the rename is visible in-band when operators `curl` the
endpoint.

Implementation: `pkg/monitoring/prometheus_prefix_migration.go`,
hooked into `PrometheusExporter.Render()`. Tests: 10 dedicated cases
in `prometheus_prefix_migration_test.go` covering default / legacy-off
/ new-off / both-off (force-fallback) states, plus the existing
`TestPrometheusExposition_containsNvidiaSeries` checking legacy names
stay present.

**Per-node knobs (environment variables, hot-reloadable — no restart):**

| Variable | Default | Effect |
|----------|---------|--------|
| `CELLS_METRICS_EMIT_LEGACY` | `1` (on) | When `0`, suppresses every `CELLSplus_*` series. Use after the operator's scrapers are fully on the new prefix. |
| `CELLS_METRICS_EMIT_CELLS` | `1` (on) | When `0`, suppresses every `CELLS_*` series. Unusual but supported (e.g. validator on an old Grafana fleet that explicitly needs legacy). |

Both-off is treated as "legacy only" with a self-observability counter
(`CELLS_metrics_emit_both_suppressed_total`) so the misconfiguration is
always visible in Grafana rather than silently killing the scrape.

**Emission state is self-observable:**

```text
CELLS_metrics_legacy_emission_enabled 1         # 0 once operator cuts over
CELLS_metrics_CELLS_emission_enabled   1         # always 1 after dual-emit lands
CELLS_metrics_emit_both_suppressed_total 0      # non-zero = misconfig
```

The same gauges are emitted under the `CELLSplus_` prefix so they are
discoverable regardless of which scraper is installed.

**Cutover timeline (indicative — subject to operator communication):**

| Release window | Default emission | Operator action |
|----------------|-------------------|------------------|
| now (deprecation window opens) | both (legacy + new) | *optional:* start porting dashboards to `CELLS_*` |
| +1 minor release | both, `CELLS_*` first-class in docs | port dashboards to `CELLS_*` |
| +2 minor releases | both | set `CELLS_METRICS_EMIT_LEGACY=0` on a canary node; verify dashboards; roll out |
| +3 minor releases | default flips to `CELLS_METRICS_EMIT_LEGACY=0` | opt back in if needed |
| +4 minor releases | legacy knob removed | — |

No metric is *renamed* — each just gains a `CELLS_*` twin for the
window. The `/api/metrics/json` JSON surface follows the same schedule
in a separate follow-up PR.

### 3.8 Binaries

| Preferred | Legacy | Notes |
|---|---|---|
| `CELLS` | `CELLSplus` | Node binary. During the deprecation window both names refer to the same build artifact — produced by the same `cmd/CELLSplus/main.go` source. Role is selected at runtime (validator default; miner when `mining_enabled=true` and the `miner` build tag is present — Phase 2.3). |
| `CELLScli` | `CELLScli` | Unchanged. |
| `CELLSminer` | — | New binary that lands in Phase 4.3. Runs the reference CPU miner and, post-audit (Phase 6), the CUDA miner. **Not** a consensus participant. |

## 4. Phase-0 values adopted in-repo

`Major Update.md` §11 lists ten open questions that counsel, the project team, and
(where relevant) governance must sign off on before mainnet genesis. For the
purpose of Phases 1–5 in-repo we adopt the plan's recommendations as **working
values**, marked throughout the documentation as "ratified per Phase 0
recommendation, awaiting counsel review". They are **not** binding until counsel
sign-off per §9 Phase 0.

> **Superseded values.** The Phase-0 working values below are historical. The
> live pilot network (pre-mainnet) has a maximum supply of **90,000,000 CELL,
> all from mining**, and no treasury allocation. The 10,000,000 CELL genesis
> treasury proposal is **withdrawn** ([Genesis Policy v1.0](GENESIS_POLICY_v1.0.md)). GPU mining is NVIDIA today
> under [Mining protocol v2](MINING_PROTOCOL_V2.md), where every proof must carry
> an NVIDIA attestation. See [CELL tokenomics](CELL_TOKENOMICS.md).

| Open question | Value adopted in-repo | Source |
|---|---|---|
| Mining algorithm | Superseded: see [Mining protocol v2](MINING_PROTOCOL_V2.md). | §5.2, §11.1 |
| NVIDIA attestation | Superseded: since mining protocol v2, every mining proof must carry an NVIDIA attestation (`nvidia-cc-v1` or `nvidia-hmac-v1`). The NGC sidecar API gate remains optional and per-operator. | §5.4, §10.1 |
| Target block time | 10 seconds (inherits from current `pkg/chain` config). | §4.2 |
| Total supply cap | Phase-0 value 100,000,000 CELL, superseded. Live: **90,000,000 CELL maximum supply, all from mining.** | §4.1 |
| Decimals | **8**, matching Bitcoin. Smallest unit named `dust` in `pkg/branding` (replaces the plan's proposal of "micell" / "cytoplasm", which were both listed as flavour alternatives). | §4.1 |
| Pre-mine | **0%.** | §4.1 |
| Genesis treasury allocation | Phase-0 value 10,000,000 CELL, superseded. **Withdrawn** by Genesis Policy v1.0: no genesis treasury. | §4.1 |
| Mining emission | **90,000,000 CELL** (the full maximum supply), halving every 4 years. | §4.1 |
| Validator fee model | No block subsidy for validators. Superseded: fees are **burned** (credited to no account) until independent validators produce blocks (Genesis Policy v1.0 §3). | §4.1 |
| Burn policy | Superseded: 100% of fees burned for now; any later change of fee destination needs a new policy version. | §4.1 |
| Coin name fallback | If "Cell" fails trademark clearance the fallback order is **QCell → Cytoplasm → Vertex**. | §10.1, §11.8 |
| Treasury custody | Superseded: no genesis treasury. Program wallets follow `TREASURY_POLICY.md` and hold only existing CELL. | §10.1, §11.10 |

Changing any of these values after Phase 3 (tokenomics doc landing) requires
updating `CELL_TOKENOMICS.md`, the emission schedule unit tests in
`pkg/chain/emission_test.go`, the audit checklist in `pkg/audit/checklist.go`,
and the landing-page tokenomics widget in a single coordinated change.

## 5. What still requires a wall-clock external dependency

These items are explicitly **not** performed by the in-repo rebrand and remain
blocked on external work called out in `Major Update.md` §9:

- Counsel sign-off on the tokenomics posture, treasury vesting legality, Stance-1
  NVIDIA framing, and trademark clearance on "CELLS" and "Cell" (Phase 0).
- Trademark search and filings in US / UK / EU.
- External cryptographic + protocol audit of the mining layer (Phase 4, gating the
  CUDA miner ship).
- Incentivized testnet with ≥ 100 concurrent home miners (Phase 4 milestone).
- Mainnet genesis ceremony (one-way door).
- Repository root directory rename from `CELLS+/` to `CELLS/` (requires a coordinated
  tag / release to avoid breaking local clones and CI paths).

## 6. Operator checklist (one-time)

For each running node:

1. Add the `CELLS_*` equivalents of every `CELLSPLUS_*` variable currently set on the
   host, leaving the legacy values in place for one restart cycle so you can
   roll back cleanly.
2. Restart the node once and confirm no deprecation warning is logged.
3. If you have monitoring or scrape scripts that set `X-CELLSPLUS-*` headers,
   update them to `X-CELLS-*`; the node accepts either during the window.
4. If you ship the NVIDIA NGC sidecar, update it to emit `CELLS_*` JSON fields
   once the sidecar release containing the rename is available. The node
   continues to accept the legacy field names in the interim.
5. Configuration files named `CELLSplus.*` continue to work. You may rename them
   to `CELLS.*` at your next scheduled maintenance window; no other change is
   required.
6. When updating SDK consumers:
   - Go: **superseded.** `sdk/go/CELLS` was never created; the single importable
     path is `github.com/blackbeardONE/CELLS/CELLS/source/sdk/go`. See §3.5.
   - JavaScript: switch from `require('CELLSplus')` to `require('CELLS')`, and
     from `CELLSPlusClient` to `CELLSClient`.

## 7. References

- `Major Update.md` — the execution plan. Kept at the repository root until the
  five in-repo phases are green, then archived to
  `CELLS/docs/docs/history/MAJOR_UPDATE_EXECUTED.md`.
- `NEXT_STEPS.md` — current phase progress.
- `CELLS/docs/docs/CELL_TOKENOMICS.md` — Cell supply schedule and fee model
  (lands in Phase 3.1).
- `CELLS/docs/docs/MINING_PROTOCOL.md` — normative mining spec
  (lands in Phase 4.1).
- `CELLS/docs/docs/NODE_ROLES.md` — validator vs miner hardware model
  (lands in Phase 3.2).
- `CELLS/source/pkg/branding/branding.go` — canonical product identity constants.
- `CELLS/source/pkg/envcompat/envcompat.go` — env-var deprecation shim.
