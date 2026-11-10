package chain

import (
	"encoding/hex"
	"errors"
	"math/big"
	mrand "math/rand"
	"strconv"
	"strings"
	"testing"

	"github.com/blackbeardONE/CELLS/pkg/mempool"
	"github.com/cloudflare/circl/sign/mldsa/mldsa87"
)

// CELLS20Rig is a block applier with the token store wired, the activation
// height set and a settable block height, as production wires it.
type CELLS20Rig struct {
	t        *testing.T
	accounts *AccountStore
	aware    *EnrollmentAwareApplier
	tokens   *CELLS20StateStore
	height   uint64
}

func newCELLS20Rig(t *testing.T, activation uint64) *CELLS20Rig {
	t.Helper()
	setCELLS20ActivationForTest(t, activation)
	r := &CELLS20Rig{t: t, accounts: NewAccountStore(), tokens: NewCELLS20StateStore(), height: activation}
	if r.height == 0 {
		r.height = 1
	}
	r.aware = NewEnrollmentAwareApplier(r.accounts, nil)
	r.aware.SetTaskStateStore(NewTaskStateStore())
	r.aware.SetStreamStateStore(NewStreamStateStore())
	r.aware.SetRecoveryCapsuleStateStore(NewRecoveryCapsuleStateStore())
	r.aware.SetCELLS20StateStore(r.tokens)
	r.aware.SetHeightFn(func() uint64 { return r.height })
	return r
}

func (r *CELLS20Rig) wallet(cell float64) CELLS20Wallet {
	r.t.Helper()
	w := newCELLS20Wallet(r.t)
	if cell > 0 {
		r.accounts.Credit(w.address, cell)
	}
	return w
}

func (r *CELLS20Rig) nonce(w CELLS20Wallet) uint64 {
	acc, ok := r.accounts.Get(w.address)
	if !ok {
		return 0
	}
	return acc.Nonce
}

// tx signs a with w's current account nonce.
func (r *CELLS20Rig) tx(w CELLS20Wallet, feeDust uint64, a CELLS20Action) *mempool.Tx {
	r.t.Helper()
	return w.signedTx(r.t, r.nonce(w), feeDust, a)
}

func (r *CELLS20Rig) mustApply(tx *mempool.Tx) {
	r.t.Helper()
	if err := r.aware.ApplyTx(tx); err != nil {
		r.t.Fatalf("apply %s: %v", tx.ID, err)
	}
}

// do applies a from w at its current nonce and must succeed.
func (r *CELLS20Rig) do(w CELLS20Wallet, a CELLS20Action) *mempool.Tx {
	r.t.Helper()
	tx := r.tx(w, 0, a)
	r.mustApply(tx)
	return tx
}

// create makes a token from w and returns its id.
func (r *CELLS20Rig) create(w CELLS20Wallet, a CELLS20Action) string {
	r.t.Helper()
	id := CELLS20TokenID(w.address, r.nonce(w))
	r.do(w, a)
	return id
}

type CELLS20Snapshot struct {
	root      string
	tokenRoot string
	accounts  string
}

func (r *CELLS20Rig) snapshot() CELLS20Snapshot {
	var b strings.Builder
	for _, acc := range r.accounts.AllAccounts() {
		b.WriteString(acc.Address + ":" + strconv.FormatFloat(acc.Balance, 'g', -1, 64) + ":" + strconv.FormatUint(acc.Nonce, 10) + ";")
	}
	return CELLS20Snapshot{root: r.aware.StateRoot(), tokenRoot: r.tokens.StateRoot(), accounts: b.String()}
}

// refused applies tx, which must fail with want and change nothing: no
// fee, no nonce, no token state, no root.
func (r *CELLS20Rig) refused(tx *mempool.Tx, want error) {
	r.t.Helper()
	before := r.snapshot()
	err := r.aware.ApplyTx(tx)
	if err == nil {
		r.t.Fatalf("transaction %s was applied, want %v", tx.ID, want)
	}
	if want != nil && !errors.Is(err, want) {
		r.t.Fatalf("got %v, want %v", err, want)
	}
	if after := r.snapshot(); after != before {
		r.t.Fatalf("refused transaction changed state:\nbefore %+v\nafter  %+v", before, after)
	}
}

func (r *CELLS20Rig) balance(id, address string) string {
	r.t.Helper()
	for _, b := range r.tokens.BalancesOf(address) {
		if b.Token == id {
			return b.Balance
		}
	}
	return "0"
}

func (r *CELLS20Rig) token(id string) CELLS20Token {
	r.t.Helper()
	tok, ok := r.tokens.Token(id)
	if !ok {
		r.t.Fatalf("token %s missing", id)
	}
	return tok
}

func TestCELLS20CreateFixedSupplyChargesFeeAndNonce(t *testing.T) {
	r := newCELLS20Rig(t, 10)
	r.height = 12
	issuer := r.wallet(5)
	tx := issuer.signedTx(t, 0, 50_000_000, CELLS20Create("GROOT", "Groot Coin", 8, "1000000", "1000000", CELLS20MintNone, ""))
	r.mustApply(tx)
	id := CELLS20TokenID(issuer.address, 0)
	tok := r.token(id)
	want := CELLS20Token{
		ID: id, Symbol: "GROOT", Name: "Groot Coin", Decimals: 8, Issuer: issuer.address, Mint: CELLS20MintNone,
		MaxSupply: "1000000", Minted: "1000000", Burned: "0", Supply: "1000000", Holders: 1, CreatedHeight: 12,
	}
	if tok != want {
		t.Fatalf("token = %+v\nwant    %+v", tok, want)
	}
	if got := r.balance(id, issuer.address); got != "1000000" {
		t.Fatalf("issuer balance = %s", got)
	}
	acc, _ := r.accounts.Get(issuer.address)
	if acc.Nonce != 1 || acc.Balance != 4.5 {
		t.Fatalf("fee and nonce: %+v (want balance 4.5, nonce 1)", acc)
	}
	if r.tokens.Count() != 1 {
		t.Fatalf("count = %d", r.tokens.Count())
	}
}

func TestCELLS20TransferAndInsufficientUnits(t *testing.T) {
	r := newCELLS20Rig(t, 1)
	alice, bob := r.wallet(1), r.wallet(1)
	carol := newCELLS20Wallet(t) // no CELL account: may still receive tokens
	id := r.create(alice, CELLS20Create("TT", "Transfer test", 0, "100", "100", CELLS20MintNone, ""))

	r.do(alice, CELLS20Transfer(id, bob.address, "30"))
	r.do(bob, CELLS20Transfer(id, carol.address, "10"))
	if a, b, c := r.balance(id, alice.address), r.balance(id, bob.address), r.balance(id, carol.address); a != "70" || b != "20" || c != "10" {
		t.Fatalf("balances alice %s bob %s carol %s", a, b, c)
	}
	// All of bob's units: bob stops being a holder.
	r.do(bob, CELLS20Transfer(id, alice.address, "20"))
	if r.token(id).Holders != 2 || r.balance(id, bob.address) != "0" {
		t.Fatalf("emptied holder kept: %+v", r.token(id))
	}
	// A transfer to oneself moves nothing and is allowed.
	r.do(alice, CELLS20Transfer(id, alice.address, "90"))
	if r.balance(id, alice.address) != "90" {
		t.Fatalf("self transfer changed the balance: %s", r.balance(id, alice.address))
	}

	r.refused(r.tx(alice, 0, CELLS20Transfer(id, bob.address, "91")), ErrCELLS20InsufficientUnits)
	r.refused(r.tx(bob, 0, CELLS20Transfer(id, alice.address, "1")), ErrCELLS20InsufficientUnits)
	r.refused(r.tx(alice, 0, CELLS20Transfer(id, alice.address, "91")), ErrCELLS20InsufficientUnits)
	if s := r.token(id).Supply; s != "100" {
		t.Fatalf("supply moved: %s", s)
	}
}

func TestCELLS20UnknownTokenRefused(t *testing.T) {
	r := newCELLS20Rig(t, 1)
	alice, bob := r.wallet(1), r.wallet(1)
	unknown := CELLS20TokenID(bob.address, 99)
	r.refused(r.tx(alice, 0, CELLS20Transfer(unknown, bob.address, "1")), ErrCELLS20UnknownToken)
	r.refused(r.tx(alice, 0, CELLS20MintAction(unknown, alice.address, "1")), ErrCELLS20UnknownToken)
	r.refused(r.tx(alice, 0, CELLS20Burn(unknown, "1")), ErrCELLS20UnknownToken)
	r.refused(r.tx(alice, 0, CELLS20Renounce(unknown)), ErrCELLS20UnknownToken)
}

func TestCELLS20IssuerMint(t *testing.T) {
	r := newCELLS20Rig(t, 1)
	issuer, other := r.wallet(1), r.wallet(1)
	id := r.create(issuer, CELLS20Create("ISS", "Issuer coin", 2, "1000", "100", CELLS20MintIssuer, ""))

	r.do(issuer, CELLS20MintAction(id, other.address, "400"))
	if b := r.balance(id, other.address); b != "400" {
		t.Fatalf("minted to other: %s", b)
	}
	r.refused(r.tx(other, 0, CELLS20MintAction(id, other.address, "1")), ErrCELLS20NotIssuer)
	r.refused(r.tx(issuer, 0, CELLS20MintAction(id, issuer.address, "501")), ErrCELLS20OverMaxSupply)
	r.do(issuer, CELLS20MintAction(id, issuer.address, "500")) // exactly up to max_supply
	tok := r.token(id)
	if tok.Minted != "1000" || tok.Supply != "1000" || tok.MaxSupply != "1000" {
		t.Fatalf("after minting to max: %+v", tok)
	}
	r.refused(r.tx(issuer, 0, CELLS20MintAction(id, issuer.address, "1")), ErrCELLS20OverMaxSupply)
}

func TestCELLS20MintRefusedForFixedSupply(t *testing.T) {
	r := newCELLS20Rig(t, 1)
	issuer := r.wallet(1)
	id := r.create(issuer, CELLS20Create("FIX", "Fixed", 0, "5", "5", CELLS20MintNone, ""))
	r.refused(r.tx(issuer, 0, CELLS20MintAction(id, issuer.address, "1")), ErrCELLS20MintingOff)
}

func TestCELLS20OpenMint(t *testing.T) {
	r := newCELLS20Rig(t, 1)
	issuer, minter, other := r.wallet(1), r.wallet(1), r.wallet(1)
	id := r.create(issuer, CELLS20Create("FAIR", "Fair launch", 0, "250", "0", CELLS20MintOpen, "100"))
	if tok := r.token(id); tok.Minted != "0" || tok.Holders != 0 || tok.MintLimit != "100" {
		t.Fatalf("open token with no initial supply: %+v", tok)
	}

	r.do(minter, CELLS20MintAction(id, minter.address, "100")) // exactly mint_limit
	r.do(issuer, CELLS20MintAction(id, issuer.address, "50"))  // the issuer mints like anyone
	r.refused(r.tx(minter, 0, CELLS20MintAction(id, minter.address, "101")), ErrCELLS20OverMintLimit)
	r.refused(r.tx(minter, 0, CELLS20MintAction(id, other.address, "1")), ErrCELLS20OpenMintRecipient)
	r.refused(r.tx(issuer, 0, CELLS20MintAction(id, other.address, "1")), ErrCELLS20OpenMintRecipient)
	r.do(other, CELLS20MintAction(id, other.address, "100")) // minted 250 = max
	r.refused(r.tx(other, 0, CELLS20MintAction(id, other.address, "1")), ErrCELLS20OverMaxSupply)
	tok := r.token(id)
	if tok.Minted != "250" || tok.Holders != 3 {
		t.Fatalf("after the fair launch: %+v", tok)
	}
}

// Burned units are gone: a mint is allowed only while minted + amount <=
// max_supply, so burning never makes room for new units.
func TestCELLS20BurnedUnitsCannotBeReminted(t *testing.T) {
	r := newCELLS20Rig(t, 1)
	issuer := r.wallet(1)
	id := r.create(issuer, CELLS20Create("BRN", "Burn", 0, "100", "100", CELLS20MintIssuer, ""))
	r.do(issuer, CELLS20Burn(id, "40"))
	tok := r.token(id)
	if tok.Minted != "100" || tok.Burned != "40" || tok.Supply != "60" || r.balance(id, issuer.address) != "60" {
		t.Fatalf("after burn: %+v balance %s", tok, r.balance(id, issuer.address))
	}
	r.refused(r.tx(issuer, 0, CELLS20MintAction(id, issuer.address, "1")), ErrCELLS20OverMaxSupply)
	r.refused(r.tx(issuer, 0, CELLS20Burn(id, "61")), ErrCELLS20InsufficientUnits)
	r.do(issuer, CELLS20Burn(id, "60"))
	if tok := r.token(id); tok.Supply != "0" || tok.Holders != 0 || tok.Burned != "100" {
		t.Fatalf("after burning everything: %+v", tok)
	}
	other := r.wallet(1)
	r.refused(r.tx(other, 0, CELLS20Burn(id, "1")), ErrCELLS20InsufficientUnits)
}

func TestCELLS20Renounce(t *testing.T) {
	r := newCELLS20Rig(t, 1)
	issuer, other := r.wallet(1), r.wallet(1)
	id := r.create(issuer, CELLS20Create("REN", "Renounce", 0, "1000", "10", CELLS20MintIssuer, ""))
	r.do(issuer, CELLS20MintAction(id, other.address, "15"))
	r.do(other, CELLS20Burn(id, "5"))

	r.refused(r.tx(other, 0, CELLS20Renounce(id)), ErrCELLS20NotIssuer)
	r.do(issuer, CELLS20Renounce(id))
	tok := r.token(id)
	if tok.Mint != CELLS20MintNone || tok.MaxSupply != "25" || tok.Minted != "25" || tok.Burned != "5" || tok.Supply != "20" {
		t.Fatalf("after renounce: %+v (max_supply must become the 25 minted so far)", tok)
	}
	r.refused(r.tx(issuer, 0, CELLS20MintAction(id, issuer.address, "1")), ErrCELLS20MintingOff)
	r.refused(r.tx(issuer, 0, CELLS20Renounce(id)), ErrCELLS20MintingOff)

	fixed := r.create(issuer, CELLS20Create("FIX", "Fixed", 0, "3", "3", CELLS20MintNone, ""))
	r.refused(r.tx(issuer, 0, CELLS20Renounce(fixed)), ErrCELLS20MintingOff)

	open := r.create(issuer, CELLS20Create("OPN", "Open", 0, "1000", "0", CELLS20MintOpen, "10"))
	r.do(other, CELLS20MintAction(open, other.address, "7"))
	r.refused(r.tx(other, 0, CELLS20Renounce(open)), ErrCELLS20NotIssuer)
	r.do(issuer, CELLS20Renounce(open))
	if tok := r.token(open); tok.Mint != CELLS20MintNone || tok.MaxSupply != "7" || tok.MintLimit != "" {
		t.Fatalf("renounced open token: %+v", tok)
	}
	r.refused(r.tx(other, 0, CELLS20MintAction(open, other.address, "1")), ErrCELLS20MintingOff)
}

// The CELL side: the sender needs an account with the fee, and the fee is
// burned (debited, credited to nobody).
func TestCELLS20FeeAndAccountRefusals(t *testing.T) {
	r := newCELLS20Rig(t, 1)
	poor := r.wallet(0.5)
	create := CELLS20Create("FEE", "Fee", 0, "1", "1", CELLS20MintNone, "")
	r.refused(poor.signedTx(t, 0, 50_000_001, create), nil) // 0.50000001 CELL > 0.5
	if r.tokens.Count() != 0 {
		t.Fatal("refused create made a token")
	}
	tx := poor.signedTx(t, 0, 50_000_000, create)
	r.mustApply(tx)
	if acc, _ := r.accounts.Get(poor.address); acc.Balance != 0 || acc.Nonce != 1 {
		t.Fatalf("fee not burned: %+v", acc)
	}
	var total float64
	for _, acc := range r.accounts.AllAccounts() {
		total += acc.Balance
	}
	if total != 0 {
		t.Fatalf("the fee went somewhere: total CELL %v", total)
	}

	stranger := newCELLS20Wallet(t) // no CELL account
	r.refused(stranger.signedTx(t, 0, 0, CELLS20Create("NOA", "No account", 0, "1", "1", CELLS20MintNone, "")), nil)
	if _, ok := r.accounts.Get(stranger.address); ok {
		t.Fatal("a refused transaction created an account")
	}
}

func TestCELLS20ReplayedNonceRefused(t *testing.T) {
	r := newCELLS20Rig(t, 1)
	alice, bob := r.wallet(1), r.wallet(1)
	create := r.tx(alice, 0, CELLS20Create("RPL", "Replay", 0, "100", "100", CELLS20MintNone, ""))
	r.mustApply(create)
	id := CELLS20TokenID(alice.address, 0)
	r.refused(create, ErrCELLS20TokenExists) // the same create again

	transfer := r.tx(alice, 0, CELLS20Transfer(id, bob.address, "10"))
	r.mustApply(transfer)
	r.refused(transfer, nil) // nonce 1 is used
	if r.balance(id, bob.address) != "10" {
		t.Fatalf("replayed transfer moved units: %s", r.balance(id, bob.address))
	}
	// A different transaction at a used nonce is refused too.
	r.refused(alice.signedTx(t, 1, 0, CELLS20Transfer(id, bob.address, "11")), nil)
	// And one at a future nonce.
	r.refused(alice.signedTx(t, 5, 0, CELLS20Transfer(id, bob.address, "1")), nil)
}

func CELLS20AccountsDump(as *AccountStore) string {
	var b strings.Builder
	for _, acc := range as.AllAccounts() {
		b.WriteString(acc.Address + ":" + strconv.FormatFloat(acc.Balance, 'g', -1, 64) + ":" + strconv.FormatUint(acc.Nonce, 10) + ";")
	}
	return b.String()
}

// judgedAsBefore applies tx through the rig's applier and, on a copy of the
// accounts, through the plain transfer path alone (AccountStore.ApplyTx),
// which is all a node without CELLS-20 does with a CELLS/token/v1
// transaction. Both must agree: an error from both or from neither, the same
// accounts, the same state root, and the token state untouched.
func (r *CELLS20Rig) judgedAsBefore(tx *mempool.Tx) {
	r.t.Helper()
	legacy := r.accounts.Clone()
	legacyErr := legacy.ApplyTx(tx)
	tokens, tokenRoot := r.tokens.Count(), r.tokens.StateRoot()
	err := r.aware.ApplyTx(tx)
	if (err == nil) != (legacyErr == nil) {
		r.t.Fatalf("applier: %v; plain transfer: %v", err, legacyErr)
	}
	if got, want := CELLS20AccountsDump(r.accounts), CELLS20AccountsDump(legacy); got != want {
		r.t.Fatalf("accounts differ from the plain transfer:\ngot  %s\nwant %s", got, want)
	}
	old := NewEnrollmentAwareApplier(legacy, nil)
	old.SetTaskStateStore(NewTaskStateStore())
	old.SetStreamStateStore(NewStreamStateStore())
	old.SetRecoveryCapsuleStateStore(NewRecoveryCapsuleStateStore())
	if got, want := r.aware.StateRoot(), old.StateRoot(); got != want {
		r.t.Fatalf("state root %s; a node without CELLS-20 has %s", got, want)
	}
	if r.tokens.Count() != tokens || r.tokens.StateRoot() != tokenRoot {
		r.t.Fatal("a transaction below the activation height touched the token state")
	}
}

// Below the activation height a CELLS/token/v1 transaction that reaches a
// block is judged as a node without CELLS-20 judges it, so the code makes no
// block invalid that is valid today. The submit check still refuses it, and
// the token rules start at the height.
func TestCELLS20BelowActivationJudgedAsBefore(t *testing.T) {
	r := newCELLS20Rig(t, 100)
	alice := r.wallet(1)
	create := CELLS20Create("ACT", "Activation", 0, "1", "1", CELLS20MintNone, "")
	r.height = 99
	if err := r.aware.CheckCELLS20Tx(r.tx(alice, 0, create)); !errors.Is(err, ErrCELLS20Inactive) {
		t.Fatalf("submit check below the height: %v", err)
	}
	early := r.tx(alice, 1_000, create)
	r.judgedAsBefore(early)
	if acc, _ := r.accounts.Get(alice.address); acc.Nonce != 1 {
		t.Fatalf("the plain transfer did not use the nonce: %+v", acc)
	}
	r.judgedAsBefore(early) // its nonce is used now: refused, as before
	if err := NewCELLS20StateStore().ApplyHistoricalTx(early, 99); !errors.Is(err, ErrCELLS20Inactive) {
		t.Fatalf("historical replay below the height: %v", err)
	}
	r.height = 100
	tx := r.tx(alice, 0, create)
	r.mustApply(tx)
	if r.tokens.Count() != 1 {
		t.Fatalf("%d tokens at the activation height", r.tokens.Count())
	}
	if err := NewCELLS20StateStore().ApplyHistoricalTx(tx, 100); err != nil {
		t.Fatalf("historical replay at the height: %v", err)
	}
}

// Activation height 0 (the default): the submit check and the store refuse
// every token transaction, and a block that carries one anyway is judged as
// before CELLS-20 at any height, with or without a height source or a token
// store.
func TestCELLS20SwitchedOffJudgesBlocksAsBefore(t *testing.T) {
	create := CELLS20Create("OFF", "Off", 0, "1", "1", CELLS20MintNone, "")
	for _, h := range []uint64{0, 1, 1 << 40} {
		r := newCELLS20Rig(t, 0)
		alice := r.wallet(2)
		r.height = h
		tx := r.tx(alice, 10_000_000, create)
		if err := r.aware.CheckCELLS20Tx(tx); !errors.Is(err, ErrCELLS20Inactive) {
			t.Fatalf("CheckCELLS20Tx at %d: %v", h, err)
		}
		r.judgedAsBefore(tx)
		r.judgedAsBefore(tx)
		r.judgedAsBefore(alice.signedTx(t, 7, 0, create)) // a future nonce
	}
	r := newCELLS20Rig(t, 0)
	alice := r.wallet(2)
	r.aware.SetHeightFn(nil)
	r.judgedAsBefore(r.tx(alice, 0, create))
	r.aware.SetCELLS20StateStore(nil)
	r.judgedAsBefore(r.tx(alice, 0, create))
	r.judgedAsBefore(newCELLS20Wallet(t).signedTx(t, 0, 0, create)) // no CELL account

	tx := r.tx(alice, 0, create)
	for _, h := range []uint64{0, 1, 1 << 40} {
		if err := NewCELLS20StateStore().ApplyHistoricalTx(tx, h); !errors.Is(err, ErrCELLS20Inactive) {
			t.Fatalf("historical replay at %d: %v", h, err)
		}
	}
	if err := NewCELLS20StateStore().ApplyTx(tx); !errors.Is(err, ErrCELLS20Inactive) {
		t.Fatalf("store ApplyTx: %v", err)
	}
}

func TestCELLS20NoHeightSourceFailsClosed(t *testing.T) {
	r := newCELLS20Rig(t, 1)
	alice := r.wallet(1)
	r.aware.SetHeightFn(nil)
	r.refused(r.tx(alice, 0, CELLS20Create("NHS", "No height", 0, "1", "1", CELLS20MintNone, "")), ErrEnrollmentHeightUnset)
}

func TestCELLS20StoreNotWiredRefused(t *testing.T) {
	setCELLS20ActivationForTest(t, 1)
	accounts := NewAccountStore()
	alice := newCELLS20Wallet(t)
	accounts.Credit(alice.address, 1)
	aware := NewEnrollmentAwareApplier(accounts, nil)
	aware.SetHeightFn(func() uint64 { return 5 })
	tx := alice.signedTx(t, 0, 0, CELLS20Create("NW", "Not wired", 0, "1", "1", CELLS20MintNone, ""))
	if err := aware.ApplyTx(tx); !errors.Is(err, ErrCELLS20StateNotWired) {
		t.Fatalf("got %v", err)
	}
	if err := aware.CheckCELLS20Tx(tx); !errors.Is(err, ErrCELLS20StateNotWired) {
		t.Fatalf("check: %v", err)
	}
	if acc, _ := accounts.Get(alice.address); acc.Nonce != 0 {
		t.Fatal("nonce moved")
	}
}

// A wrong public key or a tampered signature is refused when the block is
// applied, not only at admission.
func TestCELLS20SignatureRefusedAtApply(t *testing.T) {
	r := newCELLS20Rig(t, 1)
	alice, mallory := r.wallet(1), r.wallet(1)
	id := r.create(alice, CELLS20Create("SIG", "Signature", 0, "100", "100", CELLS20MintNone, ""))

	// Mallory signs the exact digest of a transfer of alice's units with her
	// own key: her key does not hash to alice's address.
	steal := CELLS20Transfer(id, mallory.address, "100")
	forged := CELLS20UnsignedTx(t, alice.address, r.nonce(alice), 0, steal)
	sig := make([]byte, mldsa87.SignatureSize)
	if err := mldsa87.SignTo(mallory.private, CELLS20SigningDigest(alice.address, r.nonce(alice), 0, steal), nil, true, sig); err != nil {
		t.Fatal(err)
	}
	forged.Signature, forged.PublicKey = hex.EncodeToString(sig), hex.EncodeToString(mallory.pub)
	r.refused(forged, ErrCELLS20Signature)

	tampered := r.tx(alice, 0, CELLS20Transfer(id, mallory.address, "1"))
	raw, _ := hex.DecodeString(tampered.Signature)
	raw[0] ^= 0x80
	tampered.Signature = hex.EncodeToString(raw)
	r.refused(tampered, ErrCELLS20Signature)
	if r.balance(id, mallory.address) != "0" {
		t.Fatal("units moved")
	}
}

// CheckCELLS20Tx (the submit API's check) agrees with the applier and
// changes nothing.
func TestCELLS20CheckMatchesApply(t *testing.T) {
	r := newCELLS20Rig(t, 1)
	alice, bob := r.wallet(1), r.wallet(1)
	id := r.create(alice, CELLS20Create("CHK", "Check", 0, "10", "10", CELLS20MintNone, ""))
	ok := r.tx(alice, 0, CELLS20Transfer(id, bob.address, "10"))
	tooMuch := r.tx(alice, 0, CELLS20Transfer(id, bob.address, "11"))
	stale := alice.signedTx(t, 0, 0, CELLS20Transfer(id, bob.address, "1"))
	before := r.snapshot()
	if err := r.aware.CheckCELLS20Tx(ok); err != nil {
		t.Fatalf("check of a valid transfer: %v", err)
	}
	if err := r.aware.CheckCELLS20Tx(tooMuch); !errors.Is(err, ErrCELLS20InsufficientUnits) {
		t.Fatalf("check of an overdraft: %v", err)
	}
	if err := r.aware.CheckCELLS20Tx(stale); err == nil || !strings.Contains(err.Error(), "nonce mismatch") {
		t.Fatalf("check of a stale nonce: %v", err)
	}
	if after := r.snapshot(); after != before {
		t.Fatal("CheckCELLS20Tx changed state")
	}
	r.mustApply(ok)
}

// Whatever happens, a token's balances add up to minted - burned, and
// minted never passes max_supply.
func TestCELLS20SupplyInvariant(t *testing.T) {
	r := newCELLS20Rig(t, 1)
	rng := mrand.New(mrand.NewSource(20))
	wallets := []CELLS20Wallet{r.wallet(1), r.wallet(1), r.wallet(1)}
	issuer := wallets[0]
	ids := []string{
		r.create(issuer, CELLS20Create("INV", "Issuer", 0, "1000", "100", CELLS20MintIssuer, "")),
		r.create(wallets[1], CELLS20Create("OPN", "Open", 0, "700", "0", CELLS20MintOpen, "40")),
	}
	for i := 0; i < 120; i++ {
		w := wallets[rng.Intn(len(wallets))]
		to := wallets[rng.Intn(len(wallets))]
		id := ids[rng.Intn(len(ids))]
		amount := strconv.Itoa(1 + rng.Intn(60))
		var a CELLS20Action
		switch rng.Intn(4) {
		case 0:
			a = CELLS20Transfer(id, to.address, amount)
		case 1:
			a = CELLS20MintAction(id, to.address, amount)
		case 2:
			a = CELLS20Burn(id, amount)
		default:
			a = CELLS20MintAction(id, w.address, amount)
		}
		_ = r.aware.ApplyTx(r.tx(w, 0, a))
		for _, id := range ids {
			tok := r.token(id)
			holders, _ := r.tokens.Holders(id)
			sum := new(big.Int)
			for _, h := range holders {
				b, _ := new(big.Int).SetString(h.Balance, 10)
				if b.Sign() <= 0 {
					t.Fatalf("holder %s with balance %s", h.Address, h.Balance)
				}
				sum.Add(sum, b)
			}
			minted, _ := new(big.Int).SetString(tok.Minted, 10)
			burned, _ := new(big.Int).SetString(tok.Burned, 10)
			maxSupply, _ := new(big.Int).SetString(tok.MaxSupply, 10)
			if sum.Cmp(new(big.Int).Sub(minted, burned)) != 0 || sum.String() != tok.Supply {
				t.Fatalf("step %d: balances %s != minted %s - burned %s", i, sum, minted, burned)
			}
			if minted.Cmp(maxSupply) > 0 {
				t.Fatalf("step %d: minted %s > max_supply %s", i, minted, maxSupply)
			}
		}
	}
}

// Amounts are exact up to 2^256 - 1.
func TestCELLS20LargestAmounts(t *testing.T) {
	r := newCELLS20Rig(t, 1)
	alice, bob := r.wallet(1), r.wallet(1)
	id := r.create(alice, CELLS20Create("MAX", "Max", 18, CELLS20MaxUnitsString, "1", CELLS20MintIssuer, ""))
	rest := new(big.Int).Sub(CELLS20MaxUnits, big.NewInt(1)).String()
	r.do(alice, CELLS20MintAction(id, bob.address, rest))
	if tok := r.token(id); tok.Minted != CELLS20MaxUnitsString || tok.Supply != CELLS20MaxUnitsString {
		t.Fatalf("minted to 2^256-1: %+v", tok)
	}
	r.refused(r.tx(alice, 0, CELLS20MintAction(id, bob.address, "1")), ErrCELLS20OverMaxSupply)
	r.do(alice, CELLS20Transfer(id, bob.address, "1"))
	if b := r.balance(id, bob.address); b != CELLS20MaxUnitsString {
		t.Fatalf("bob holds %s", b)
	}
}

func TestCELLS20ReadViews(t *testing.T) {
	r := newCELLS20Rig(t, 1)
	alice, bob, carol := r.wallet(1), r.wallet(1), r.wallet(1)
	r.height = 5
	first := r.create(alice, CELLS20Create("ONE", "First", 0, "100", "100", CELLS20MintNone, ""))
	r.height = 9
	second := r.create(bob, CELLS20Create("TWO", "Second", 0, "100", "100", CELLS20MintNone, ""))
	third := r.create(carol, CELLS20Create("THREE", "Third", 0, "100", "100", CELLS20MintNone, ""))

	list := r.tokens.Tokens()
	if len(list) != 3 || list[2].ID != first {
		t.Fatalf("tokens not newest first: %+v", list)
	}
	sameHeight := []string{second, third}
	if sameHeight[0] > sameHeight[1] {
		sameHeight[0], sameHeight[1] = sameHeight[1], sameHeight[0]
	}
	if list[0].ID != sameHeight[0] || list[1].ID != sameHeight[1] {
		t.Fatal("tokens of one height must be ordered by id")
	}

	r.do(alice, CELLS20Transfer(first, carol.address, "30"))
	r.do(alice, CELLS20Transfer(first, bob.address, "30"))
	holders, ok := r.tokens.Holders(first)
	if !ok || len(holders) != 3 || holders[0].Address != alice.address || holders[0].Balance != "40" {
		t.Fatalf("holders: %+v", holders)
	}
	tied := []string{bob.address, carol.address}
	if tied[0] > tied[1] {
		tied[0], tied[1] = tied[1], tied[0]
	}
	if holders[1].Address != tied[0] || holders[2].Address != tied[1] {
		t.Fatalf("equal balances must be ordered by address: %+v", holders)
	}
	if _, ok := r.tokens.Holders(CELLS20TokenID(alice.address, 77)); ok {
		t.Fatal("holders of an unknown token")
	}
	balances := r.tokens.BalancesOf(carol.address)
	if len(balances) != 2 || balances[0].Token > balances[1].Token {
		t.Fatalf("carol's balances: %+v", balances)
	}
	if got := r.tokens.BalancesOf(strings.Repeat("0", 64)); len(got) != 0 {
		t.Fatalf("balances of a stranger: %+v", got)
	}
}
