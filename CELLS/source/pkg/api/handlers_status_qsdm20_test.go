package api

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/blackbeardONE/CELLS/pkg/chain"
)

func TestStatus_ReportsCELLS20Activation(t *testing.T) {
	prev := chain.CELLS20ActivationHeight()
	t.Cleanup(func() { chain.SetCELLS20ActivationHeight(prev) })
	h := &Handlers{}

	chain.SetCELLS20ActivationHeight(0)
	info := h.buildConsensusAuthInfo(1 << 40)
	if info.CELLS20ActivationHeight != 0 || info.CELLS20Active {
		t.Fatalf("switched off: %+v", info)
	}
	raw, err := json.Marshal(info)
	if err != nil || !strings.Contains(string(raw), `"CELLS20_activation_height":0`) ||
		!strings.Contains(string(raw), `"CELLS20_active":false`) {
		t.Fatalf("status JSON: %s %v", raw, err)
	}

	chain.SetCELLS20ActivationHeight(50)
	if info := h.buildConsensusAuthInfo(48); info.CELLS20ActivationHeight != 50 || info.CELLS20Active {
		t.Fatalf("tip 48: %+v", info)
	}
	info = h.buildConsensusAuthInfo(49)
	if !info.CELLS20Active {
		t.Fatalf("tip 49 (next block 50 accepts tokens): %+v", info)
	}
	raw, err = json.Marshal(info)
	if err != nil || !strings.Contains(string(raw), `"CELLS20_activation_height":50`) ||
		!strings.Contains(string(raw), `"CELLS20_active":true`) {
		t.Fatalf("status JSON: %s %v", raw, err)
	}
}
