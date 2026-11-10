package monitoring

import (
	"strings"
	"testing"
)

func TestPrometheusExposition_containsNvidiaSeries(t *testing.T) {
	s := PrometheusExposition()
	for _, sub := range []string{
		"CELLS_nvidia_lock_http_blocks_total",
		"CELLS_nvidia_lock_p2p_rejects_total",
		"CELLS_ngc_challenge_issued_total",
		"CELLS_ngc_proof_ingest_accepted_total",
		"CELLS_ngc_proof_ingest_rejected_total",
		"# TYPE CELLS_ngc_ingest_nonce_pool_size gauge",
		"CELLS_submesh_p2p_reject_route_total",
		"CELLS_submesh_api_wallet_reject_route_total",
		"CELLS_mesh_companion_publish_total",
		"CELLS_p2p_wallet_ingress_dedupe_skip_total",
		"CELLS_hot_reload_apply_success_total",
		"CELLS_hot_reload_dry_run_total",
		"CELLS_hot_reload_last_dry_run_changed",
	} {
		if !strings.Contains(s, sub) {
			t.Fatalf("exposition missing %q", sub)
		}
	}
}
