package v2wiring_test

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/blackbeardONE/CELLS/internal/v2wiring"
	"github.com/blackbeardONE/CELLS/pkg/api"
	"github.com/blackbeardONE/CELLS/pkg/chain"
	"github.com/blackbeardONE/CELLS/pkg/mempool"
	"github.com/cloudflare/circl/sign/mldsa/mldsa87"
)

type CELLS20WireWallet struct {
	private *mldsa87.PrivateKey
	pub     []byte
	address string
}

func newCELLS20WireWallet(t *testing.T) CELLS20WireWallet {
	t.Helper()
	public, private, err := mldsa87.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	pub, _ := public.MarshalBinary()
	sum := sha256.Sum256(pub)
	return CELLS20WireWallet{private: private, pub: pub, address: hex.EncodeToString(sum[:])}
}

func (w CELLS20WireWallet) tx(t *testing.T, nonce uint64, a chain.CELLS20Action) *mempool.Tx {
	t.Helper()
	sig := make([]byte, mldsa87.SignatureSize)
	if err := mldsa87.SignTo(w.private, chain.CELLS20SigningDigest(w.address, nonce, 0, a), nil, true, sig); err != nil {
		t.Fatal(err)
	}
	tx, err := api.CELLS20TxFromRequest(api.CELLS20SubmitRequest{
		Sender: w.address, Nonce: nonce, Payload: a,
		PublicKey: hex.EncodeToString(w.pub), Signature: hex.EncodeToString(sig),
	})
	if err != nil {
		t.Fatal(err)
	}
	return tx
}

// Wire installs the token store on the applier, the CELLS-20 admission layer
// (judged at the producer's next block) and the API; ReinstallAdmissionGate
// keeps the layer and its height source.
func TestWire_CELLS20(t *testing.T) {
	r := buildRig(t, 20)
	prev := chain.CELLS20ActivationHeight()
	t.Cleanup(func() {
		chain.SetCELLS20ActivationHeight(prev)
		api.SetCELLS20Mempool(nil)
		api.SetCELLS20StateProvider(nil)
		api.SetCELLS20TxChecker(nil)
	})
	if r.w.CELLS20State == nil || r.w.Aware.CELLS20StateStore() != r.w.CELLS20State {
		t.Fatal("Wire did not install the CELLS-20 token store on the applier")
	}

	issuer := newCELLS20WireWallet(t)
	r.accounts.Credit(issuer.address, 1)
	create := issuer.tx(t, 0, chain.CELLS20Action{Action: chain.CELLS20ActionCreate, Symbol: "WIRE", Name: "Wired",
		Decimals: chain.CELLS20Decimals(0), MaxSupply: "10", InitialSupply: "10", Mint: chain.CELLS20MintNone})

	chain.SetCELLS20ActivationHeight(0)
	if err := r.pool.Add(create); err == nil || !strings.Contains(err.Error(), "not active") {
		t.Fatalf("switched off, the pool admitted a token transaction: %v", err)
	}

	// Seal block 0 so the next block is 1, then switch tokens on from 1.
	if err := r.pool.Add(&mempool.Tx{ID: "wire-plain", Sender: tAlice, Recipient: "bob", Amount: 1, Fee: 0.001, Nonce: 0}); err != nil {
		t.Fatal(err)
	}
	produce(t, r)
	chain.SetCELLS20ActivationHeight(2)
	if err := r.pool.Add(create); err == nil || !strings.Contains(err.Error(), "below the activation height") {
		t.Fatalf("next block 1 is below 2: %v", err)
	}
	chain.SetCELLS20ActivationHeight(1)
	if err := r.pool.Add(create); err != nil {
		t.Fatalf("active at the next block: %v", err)
	}
	blk := produce(t, r)
	if len(blk.Transactions) != 1 || blk.Transactions[0].ID != create.ID {
		t.Fatalf("block did not carry the token transaction: %+v", blk.Transactions)
	}
	id := chain.CELLS20TokenID(issuer.address, 0)
	if tok, ok := r.w.CELLS20State.Token(id); !ok || tok.CreatedHeight != 1 {
		t.Fatalf("token after the block: %+v %v", tok, ok)
	}

	// ReinstallAdmissionGate (what main() calls) keeps the layer and the
	// producer's height source.
	v2wiring.ReinstallAdmissionGate(r.pool, nil)
	burn := issuer.tx(t, 1, chain.CELLS20Action{Action: chain.CELLS20ActionBurn, Token: id, Amount: "1"})
	chain.SetCELLS20ActivationHeight(3)
	if err := r.pool.Add(burn); err == nil || !strings.Contains(err.Error(), "below the activation height") {
		t.Fatalf("after reinstall, next block 2 is below 3: %v", err)
	}
	chain.SetCELLS20ActivationHeight(2)
	if err := r.pool.Add(burn); err != nil {
		t.Fatalf("after reinstall, active at the next block: %v", err)
	}

	// A pool Wire never configured has no height source: fail closed.
	other := mempool.New(mempool.DefaultConfig())
	v2wiring.ReinstallAdmissionGate(other, nil)
	if err := other.Add(burn); err == nil {
		t.Fatal("a pool with no height source admitted a token transaction")
	}
}
