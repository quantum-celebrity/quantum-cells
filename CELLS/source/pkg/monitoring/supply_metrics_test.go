package monitoring

import (
	"strings"
	"testing"

	"github.com/blackbeardONE/CELLS/pkg/monitoring/supplymetrics"
)

type fakeSupply struct{ s supplymetrics.SupplySnapshot }

func (f fakeSupply) SupplySnapshot() supplymetrics.SupplySnapshot { return f.s }

// The collector must emit nothing when unwired, and real series when wired.
// Registering a provider that nothing renders is the "shipped inert" pattern
// this repo already has two open criticals for.
func TestSupplyCollector_emitsOnlyWhenWired(t *testing.T) {
	RegisterSupplyProvider(nil)
	if m := supplyPrometheusMetrics(); m != nil {
		t.Fatalf("unwired collector must emit nothing, got %d metrics", len(m))
	}

	RegisterSupplyProvider(fakeSupply{supplymetrics.SupplySnapshot{
		TotalCELL: 1234.5, Accounts: 7, NegativeAccounts: 0,
	}})
	t.Cleanup(func() { RegisterSupplyProvider(nil) })

	got := supplyPrometheusMetrics()
	if len(got) != 3 {
		t.Fatalf("want 3 series, got %d", len(got))
	}
	byName := map[string]Metric{}
	for _, m := range got {
		byName[m.Name] = m
		if !strings.HasPrefix(m.Name, "CELLS_cell_supply") {
			t.Errorf("unexpected series name %q", m.Name)
		}
		if m.Help == "" {
			t.Errorf("%s has no help text", m.Name)
		}
	}
	if byName["CELLS_cell_supply_total"].Value != 1234.5 {
		t.Errorf("total = %v, want 1234.5", byName["CELLS_cell_supply_total"].Value)
	}
	if byName["CELLS_cell_supply_accounts"].Value != 7 {
		t.Errorf("accounts = %v, want 7", byName["CELLS_cell_supply_accounts"].Value)
	}
}
