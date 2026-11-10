package main

// pq_only_wiring.go: the node-process surface of the Ed25519 retirement
// (pkg/chain/txsig.go and pkg/chain/stream_ed25519_retirement.go):
// CELLS_tx_gossip_ed25519_rejected_total and
// CELLS_stream_ed25519_retirement_height.

import (
	"github.com/blackbeardONE/CELLS/pkg/chain"
	"github.com/blackbeardONE/CELLS/pkg/monitoring"
)

// pqOnlyMetricsCollector exports the Ed25519 SignedTx refusals and the
// configured stream session-key retirement height.
func pqOnlyMetricsCollector() monitoring.MetricCollector {
	return func() []monitoring.Metric {
		return []monitoring.Metric{
			{Name: "CELLS_tx_gossip_ed25519_rejected_total", Type: monitoring.MetricCounter,
				Help:  "SignedTx refused because Ed25519 transaction signatures are retired (ML-DSA-87 only)",
				Value: float64(chain.TxSigEd25519Rejected())},
			{Name: "CELLS_stream_ed25519_retirement_height", Type: monitoring.MetricGauge,
				Help:  "Configured height from which CELL stream opens (Ed25519 session keys) are refused (0 = not scheduled)",
				Value: float64(chain.StreamEd25519RetirementHeight())},
		}
	}
}
