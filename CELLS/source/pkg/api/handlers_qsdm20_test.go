package api

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/blackbeardONE/CELLS/pkg/chain"
	"github.com/blackbeardONE/CELLS/pkg/mempool"
	"github.com/cloudflare/circl/sign/mldsa/mldsa87"
)

type CELLS20APIWallet struct {
	private *mldsa87.PrivateKey
	pub     []byte
	address string
}

func newCELLS20APIWallet(t *testing.T) CELLS20APIWallet {
	t.Helper()
	public, private, err := mldsa87.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	pub, _ := public.MarshalBinary()
	sum := sha256.Sum256(pub)
	return CELLS20APIWallet{private: private, pub: pub, address: hex.EncodeToString(sum[:])}
}

// request signs a submit body the way a wallet does: the signing digest of
// sender, nonce, fee and payload.
func (w CELLS20APIWallet) request(t *testing.T, nonce, feeDust uint64, a chain.CELLS20Action) CELLS20SubmitRequest {
	t.Helper()
	sig := make([]byte, mldsa87.SignatureSize)
	if err := mldsa87.SignTo(w.private, chain.CELLS20SigningDigest(w.address, nonce, feeDust, a), nil, true, sig); err != nil {
		t.Fatal(err)
	}
	return CELLS20SubmitRequest{
		Sender: w.address, Nonce: nonce, FeeDust: feeDust, Payload: a,
		PublicKey: hex.EncodeToString(w.pub), Signature: hex.EncodeToString(sig),
	}
}

// CELLS20APIRig wires the handlers as internal/v2wiring does: the token
// store as the read provider, the block applier as the checker and a
// mempool with the CELLS-20 admission layer.
type CELLS20APIRig struct {
	t        *testing.T
	accounts *chain.AccountStore
	aware    *chain.EnrollmentAwareApplier
	tokens   *chain.CELLS20StateStore
	pool     *mempool.Mempool
	mux      *http.ServeMux
	height   uint64
}

func newCELLS20APIRig(t *testing.T, activation uint64) *CELLS20APIRig {
	t.Helper()
	prev := chain.CELLS20ActivationHeight()
	chain.SetCELLS20ActivationHeight(activation)
	r := &CELLS20APIRig{t: t, accounts: chain.NewAccountStore(), tokens: chain.NewCELLS20StateStore(), height: 1}
	if activation > 1 {
		r.height = activation
	}
	r.aware = chain.NewEnrollmentAwareApplier(r.accounts, nil)
	r.aware.SetCELLS20StateStore(r.tokens)
	r.aware.SetHeightFn(func() uint64 { return r.height })
	r.pool = mempool.New(mempool.DefaultConfig())
	r.pool.SetAdmissionChecker(chain.CELLS20AdmissionChecker(r.aware.NextBlockHeight, nil))
	SetCELLS20Mempool(r.pool)
	SetCELLS20StateProvider(r.tokens)
	SetCELLS20TxChecker(r.aware)
	t.Cleanup(func() {
		chain.SetCELLS20ActivationHeight(prev)
		SetCELLS20Mempool(nil)
		SetCELLS20StateProvider(nil)
		SetCELLS20TxChecker(nil)
	})
	h := &Handlers{}
	r.mux = http.NewServeMux()
	r.mux.HandleFunc("/api/v1/CELLS20/tokens", h.CELLS20TokensHandler)
	r.mux.HandleFunc("/api/v1/CELLS20/tokens/", h.CELLS20TokenRouteHandler)
	r.mux.HandleFunc("/api/v1/CELLS20/balances/", h.CELLS20BalancesHandler)
	r.mux.HandleFunc("/api/v1/CELLS20/submit", h.CELLS20SubmitHandler)
	return r
}

func (r *CELLS20APIRig) wallet(cell float64) CELLS20APIWallet {
	w := newCELLS20APIWallet(r.t)
	if cell > 0 {
		r.accounts.Credit(w.address, cell)
	}
	return w
}

func (r *CELLS20APIRig) nonce(w CELLS20APIWallet) uint64 {
	acc, ok := r.accounts.Get(w.address)
	if !ok {
		return 0
	}
	return acc.Nonce
}

func (r *CELLS20APIRig) serve(method, path string, body []byte) *httptest.ResponseRecorder {
	r.t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.mux.ServeHTTP(rec, req)
	return rec
}

func (r *CELLS20APIRig) submit(v any) *httptest.ResponseRecorder {
	r.t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		r.t.Fatal(err)
	}
	return r.serve(http.MethodPost, "/api/v1/CELLS20/submit", raw)
}

// send submits a, signed by w at its current nonce, applies it as a block
// would and returns the submit response.
func (r *CELLS20APIRig) send(w CELLS20APIWallet, a chain.CELLS20Action) CELLS20SubmitResponse {
	r.t.Helper()
	rec := r.submit(w.request(r.t, r.nonce(w), 0, a))
	if rec.Code != http.StatusOK {
		r.t.Fatalf("submit %s: %d %s", a.Action, rec.Code, rec.Body.String())
	}
	var resp CELLS20SubmitResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		r.t.Fatal(err)
	}
	r.commit(resp.TxID)
	return resp
}

// commit applies a pooled transaction as block production does.
func (r *CELLS20APIRig) commit(txID string) {
	r.t.Helper()
	tx, ok := r.pool.Get(txID)
	if !ok {
		r.t.Fatalf("transaction %s is not in the mempool", txID)
	}
	if err := r.aware.ApplyTx(tx); err != nil {
		r.t.Fatalf("apply %s: %v", txID, err)
	}
	_ = r.pool.Remove(txID)
}

func (r *CELLS20APIRig) getJSON(path string, out any) {
	r.t.Helper()
	rec := r.serve(http.MethodGet, path, nil)
	if rec.Code != http.StatusOK {
		r.t.Fatalf("GET %s: %d %s", path, rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		r.t.Fatalf("GET %s: Cache-Control %q", path, rec.Header().Get("Cache-Control"))
	}
	if err := json.Unmarshal(rec.Body.Bytes(), out); err != nil {
		r.t.Fatal(err)
	}
}

func createAction(symbol string, maxSupply, initial, mint, limit string) chain.CELLS20Action {
	return chain.CELLS20Action{
		Action: chain.CELLS20ActionCreate, Symbol: symbol, Name: symbol + " token", Decimals: chain.CELLS20Decimals(6),
		MaxSupply: maxSupply, InitialSupply: initial, Mint: mint, MintLimit: limit,
	}
}

func TestCELLS20SubmitAndRead(t *testing.T) {
	r := newCELLS20APIRig(t, 1)
	alice, bob := r.wallet(2), r.wallet(1)

	req := alice.request(t, 0, 25_000_000, createAction("GROOT", "1000", "600", chain.CELLS20MintIssuer, ""))
	rec := r.submit(req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	var created CELLS20SubmitResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	id := chain.CELLS20TokenID(alice.address, 0)
	digest := chain.CELLS20SigningDigest(alice.address, 0, 25_000_000, req.Payload)
	if created.Token != id || created.TxID != "CELLS20-"+hex.EncodeToString(digest) || created.Action != "create" ||
		created.Status != "accepted" || created.MempoolStatus != "submitted" || created.Nonce != 0 {
		t.Fatalf("create response: %+v", created)
	}
	// The same transaction again while it is pending: a duplicate.
	again := r.submit(req)
	var dup CELLS20SubmitResponse
	_ = json.Unmarshal(again.Body.Bytes(), &dup)
	if again.Code != http.StatusOK || dup.MempoolStatus != "duplicate" || dup.TxID != created.TxID {
		t.Fatalf("resubmit: %d %s", again.Code, again.Body.String())
	}
	r.commit(created.TxID)
	if acc, _ := r.accounts.Get(alice.address); acc.Balance != 1.75 || acc.Nonce != 1 {
		t.Fatalf("fee and nonce after create: %+v", acc)
	}

	r.send(alice, chain.CELLS20Action{Action: chain.CELLS20ActionTransfer, Token: id, To: bob.address, Amount: "100"})
	r.send(alice, chain.CELLS20Action{Action: chain.CELLS20ActionMint, Token: id, To: bob.address, Amount: "50"})
	burn := r.send(bob, chain.CELLS20Action{Action: chain.CELLS20ActionBurn, Token: id, Amount: "30"})
	if burn.Token != id {
		t.Fatalf("burn response token: %+v", burn)
	}

	var list CELLS20TokensResponse
	r.getJSON("/api/v1/CELLS20/tokens", &list)
	if len(list.Tokens) != 1 || list.HasMore || list.NextCursor != "" || list.StateRoot != r.tokens.StateRoot() ||
		list.Runtime != "CELLS-native" || list.Source != "chain" {
		t.Fatalf("token list: %+v", list)
	}
	var one CELLS20TokenResponse
	r.getJSON("/api/v1/CELLS20/tokens/"+id, &one)
	want := chain.CELLS20Token{ID: id, Symbol: "GROOT", Name: "GROOT token", Decimals: 6, Issuer: alice.address,
		Mint: chain.CELLS20MintIssuer, MaxSupply: "1000", Minted: "650", Burned: "30", Supply: "620", Holders: 2, CreatedHeight: 1}
	if one.Token != want {
		t.Fatalf("token: %+v\nwant   %+v", one.Token, want)
	}
	var holders CELLS20HoldersResponse
	r.getJSON("/api/v1/CELLS20/tokens/"+id+"/holders", &holders)
	if len(holders.Holders) != 2 || holders.Holders[0] != (chain.CELLS20Holder{Address: alice.address, Balance: "500"}) ||
		holders.Holders[1] != (chain.CELLS20Holder{Address: bob.address, Balance: "120"}) || holders.Token != id {
		t.Fatalf("holders: %+v", holders)
	}
	var balances CELLS20BalancesResponse
	r.getJSON("/api/v1/CELLS20/balances/"+bob.address, &balances)
	if len(balances.Balances) != 1 || balances.Balances[0] != (chain.CELLS20Balance{Token: id, Symbol: "GROOT",
		Name: "GROOT token", Decimals: 6, Balance: "120"}) || balances.Address != bob.address {
		t.Fatalf("balances: %+v", balances)
	}
	stranger := strings.Repeat("0", 64)
	var none CELLS20BalancesResponse
	r.getJSON("/api/v1/CELLS20/balances/"+stranger, &none)
	if none.Balances == nil || len(none.Balances) != 0 {
		t.Fatalf("balances of an address with no tokens: %+v", none)
	}
}

func TestCELLS20SubmitRefusals(t *testing.T) {
	r := newCELLS20APIRig(t, 1)
	alice, bob := r.wallet(1), r.wallet(1)
	id := r.send(alice, createAction("REF", "100", "100", chain.CELLS20MintNone, "")).Token
	transfer := chain.CELLS20Action{Action: chain.CELLS20ActionTransfer, Token: id, To: bob.address, Amount: "1"}

	expect := func(name string, rec *httptest.ResponseRecorder, code int, contains string) {
		t.Helper()
		if rec.Code != code || !strings.Contains(rec.Body.String(), contains) {
			t.Fatalf("%s: %d %s (want %d containing %q)", name, rec.Code, rec.Body.String(), code, contains)
		}
	}
	tampered := alice.request(t, r.nonce(alice), 0, transfer)
	sig, _ := hex.DecodeString(tampered.Signature)
	sig[0] ^= 0x80
	tampered.Signature = hex.EncodeToString(sig)
	expect("tampered signature", r.submit(tampered), http.StatusUnprocessableEntity, "signature")

	wrongKey := alice.request(t, r.nonce(alice), 0, transfer)
	wrongKey.PublicKey = hex.EncodeToString(bob.pub)
	expect("wrong public key", r.submit(wrongKey), http.StatusUnprocessableEntity, "does not hash to the sender")

	expect("stale nonce", r.submit(alice.request(t, 0, 0, transfer)), http.StatusUnprocessableEntity, "nonce mismatch")
	expect("future nonce", r.submit(alice.request(t, 9, 0, transfer)), http.StatusUnprocessableEntity, "nonce mismatch")
	expect("fee over balance", r.submit(alice.request(t, r.nonce(alice), 200_000_000, transfer)), http.StatusUnprocessableEntity, "insufficient balance")
	expect("no CELL account", r.submit(newCELLS20APIWallet(t).request(t, 0, 0, createAction("NOA", "1", "1", chain.CELLS20MintNone, ""))),
		http.StatusUnprocessableEntity, "not found")

	unknown := transfer
	unknown.Token = strings.Repeat("ab", 32)
	expect("unknown token", r.submit(alice.request(t, r.nonce(alice), 0, unknown)), http.StatusUnprocessableEntity, "unknown CELLS-20 token")
	tooMuch := transfer
	tooMuch.Amount = "101"
	expect("too many units", r.submit(alice.request(t, r.nonce(alice), 0, tooMuch)), http.StatusUnprocessableEntity, "not enough token units")
	mint := chain.CELLS20Action{Action: chain.CELLS20ActionMint, Token: id, To: bob.address, Amount: "1"}
	expect("mint on fixed supply", r.submit(alice.request(t, r.nonce(alice), 0, mint)), http.StatusUnprocessableEntity, "minting is off")
	expect("renounce by another", r.submit(bob.request(t, r.nonce(bob), 0, chain.CELLS20Action{Action: chain.CELLS20ActionRenounce, Token: id})),
		http.StatusUnprocessableEntity, "only the token's issuer")
	bad := createAction("lower", "1", "1", chain.CELLS20MintNone, "")
	bad.Symbol = "lower"
	expect("bad symbol", r.submit(alice.request(t, r.nonce(alice), 0, bad)), http.StatusUnprocessableEntity, "symbol")
	expect("fee with no CELL value", r.submit(alice.request(t, r.nonce(alice), math.MaxUint64, transfer)), http.StatusUnprocessableEntity, "fee")

	raw, _ := json.Marshal(alice.request(t, r.nonce(alice), 0, transfer))
	withField := bytes.Replace(raw, []byte(`"sender"`), []byte(`"memo":"x","sender"`), 1)
	expect("unknown field", r.serve(http.MethodPost, "/api/v1/CELLS20/submit", withField), http.StatusBadRequest, "unknown field")
	payloadField := bytes.Replace(raw, []byte(`"action":"transfer"`), []byte(`"action":"transfer","memo":"x"`), 1)
	expect("unknown payload field", r.serve(http.MethodPost, "/api/v1/CELLS20/submit", payloadField), http.StatusBadRequest, "unknown field")
	expect("trailing data", r.serve(http.MethodPost, "/api/v1/CELLS20/submit", append(raw, []byte(" {}")...)), http.StatusBadRequest, "trailing")
	expect("not JSON", r.serve(http.MethodPost, "/api/v1/CELLS20/submit", []byte("nope")), http.StatusBadRequest, "invalid")
	if rec := r.serve(http.MethodGet, "/api/v1/CELLS20/submit", nil); rec.Code != http.StatusMethodNotAllowed || rec.Header().Get("Allow") != http.MethodPost {
		t.Fatalf("GET submit: %d allow %q", rec.Code, rec.Header().Get("Allow"))
	}
	if r.pool.Size() != 0 {
		t.Fatalf("refused submissions reached the mempool: %d", r.pool.Size())
	}
	// Nothing above changed alice's account.
	if acc, _ := r.accounts.Get(alice.address); acc.Nonce != 1 || acc.Balance != 1 {
		t.Fatalf("alice after refusals: %+v", acc)
	}
}

// Switched off (activation height 0) or below the height, every submit is
// refused, whatever it carries, and nothing reaches the mempool.
func TestCELLS20SubmitRefusedBelowActivation(t *testing.T) {
	r := newCELLS20APIRig(t, 0)
	alice := r.wallet(1)
	create := createAction("OFF", "1", "1", chain.CELLS20MintNone, "")
	rec := r.submit(alice.request(t, 0, 0, create))
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "not active") {
		t.Fatalf("switched off: %d %s", rec.Code, rec.Body.String())
	}
	chain.SetCELLS20ActivationHeight(10)
	r.height = 9
	rec = r.submit(alice.request(t, 0, 0, create))
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "below the activation height") {
		t.Fatalf("below the height: %d %s", rec.Code, rec.Body.String())
	}
	if r.pool.Size() != 0 {
		t.Fatal("a refused token transaction reached the mempool")
	}
	r.height = 10
	if rec := r.submit(alice.request(t, 0, 0, create)); rec.Code != http.StatusOK {
		t.Fatalf("at the height: %d %s", rec.Code, rec.Body.String())
	}
	// Admission also refuses at the mempool, for every path into it.
	chain.SetCELLS20ActivationHeight(0)
	tx, err := CELLS20TxFromRequest(r.wallet(1).request(t, 0, 0, createAction("GOS", "1", "1", chain.CELLS20MintNone, "")))
	if err != nil {
		t.Fatal(err)
	}
	if err := r.pool.Add(tx); err == nil || !strings.Contains(err.Error(), "not active") {
		t.Fatalf("mempool admission while switched off: %v", err)
	}
}

func TestCELLS20NotConfigured(t *testing.T) {
	SetCELLS20Mempool(nil)
	SetCELLS20StateProvider(nil)
	SetCELLS20TxChecker(nil)
	h := &Handlers{}
	for _, c := range []struct {
		method, path string
		handler      http.HandlerFunc
	}{
		{http.MethodPost, "/api/v1/CELLS20/submit", h.CELLS20SubmitHandler},
		{http.MethodGet, "/api/v1/CELLS20/tokens", h.CELLS20TokensHandler},
		{http.MethodGet, "/api/v1/CELLS20/tokens/" + strings.Repeat("ab", 32), h.CELLS20TokenRouteHandler},
		{http.MethodGet, "/api/v1/CELLS20/balances/" + strings.Repeat("ab", 32), h.CELLS20BalancesHandler},
	} {
		rec := httptest.NewRecorder()
		c.handler(rec, httptest.NewRequest(c.method, c.path, strings.NewReader("{}")))
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("%s %s: %d", c.method, c.path, rec.Code)
		}
	}
}

func TestCELLS20ReadErrors(t *testing.T) {
	r := newCELLS20APIRig(t, 1)
	alice := r.wallet(1)
	id := r.send(alice, createAction("ERR", "10", "10", chain.CELLS20MintNone, "")).Token
	cases := []struct {
		method, path string
		code         int
	}{
		{http.MethodGet, "/api/v1/CELLS20/tokens/" + strings.ToUpper(id), http.StatusBadRequest},
		{http.MethodGet, "/api/v1/CELLS20/tokens/abc", http.StatusBadRequest},
		{http.MethodGet, "/api/v1/CELLS20/tokens/", http.StatusBadRequest},
		{http.MethodGet, "/api/v1/CELLS20/tokens/" + strings.Repeat("ab", 32), http.StatusNotFound},
		{http.MethodGet, "/api/v1/CELLS20/tokens/" + strings.Repeat("ab", 32) + "/holders", http.StatusNotFound},
		{http.MethodGet, "/api/v1/CELLS20/tokens/" + id + "/supply", http.StatusNotFound},
		{http.MethodGet, "/api/v1/CELLS20/tokens/" + id + "/holders/x", http.StatusNotFound},
		{http.MethodGet, "/api/v1/CELLS20/tokens/" + id + "/holders?limit=0", http.StatusBadRequest},
		{http.MethodGet, "/api/v1/CELLS20/tokens/" + id + "/holders?cursor=x", http.StatusBadRequest},
		{http.MethodGet, "/api/v1/CELLS20/tokens/" + id + "/holders?cursor=-1:" + id, http.StatusBadRequest},
		{http.MethodGet, "/api/v1/CELLS20/tokens?limit=-1", http.StatusBadRequest},
		{http.MethodGet, "/api/v1/CELLS20/tokens?limit=two", http.StatusBadRequest},
		{http.MethodGet, "/api/v1/CELLS20/tokens?cursor=" + id, http.StatusBadRequest},
		{http.MethodGet, "/api/v1/CELLS20/tokens?cursor=x:" + id, http.StatusBadRequest},
		{http.MethodGet, "/api/v1/CELLS20/balances/" + strings.ToUpper(alice.address), http.StatusBadRequest},
		{http.MethodGet, "/api/v1/CELLS20/balances/", http.StatusBadRequest},
		{http.MethodGet, "/api/v1/CELLS20/balances/" + alice.address + "/x", http.StatusBadRequest},
		{http.MethodPost, "/api/v1/CELLS20/tokens", http.StatusMethodNotAllowed},
		{http.MethodPost, "/api/v1/CELLS20/tokens/" + id, http.StatusMethodNotAllowed},
		{http.MethodDelete, "/api/v1/CELLS20/balances/" + alice.address, http.StatusMethodNotAllowed},
	}
	for _, c := range cases {
		if rec := r.serve(c.method, c.path, nil); rec.Code != c.code {
			t.Errorf("%s %s: %d %s, want %d", c.method, c.path, rec.Code, rec.Body.String(), c.code)
		}
	}
}

// Paging with limit and next_cursor visits every item once, in order, and a
// token created between two pages does not shift the pages that follow.
func TestCELLS20ReadPagination(t *testing.T) {
	r := newCELLS20APIRig(t, 1)
	alice := r.wallet(10)
	var ids []string
	for i, h := range []uint64{3, 5, 5, 5, 8, 9} {
		r.height = h
		ids = append(ids, r.send(alice, createAction("P"+strconv.Itoa(i), "1000", "1000", chain.CELLS20MintNone, "")).Token)
	}
	var all CELLS20TokensResponse
	r.getJSON("/api/v1/CELLS20/tokens", &all)
	if len(all.Tokens) != 6 || all.HasMore {
		t.Fatalf("unpaged list: %+v", all)
	}
	for i := 1; i < len(all.Tokens); i++ {
		a, b := all.Tokens[i-1], all.Tokens[i]
		if a.CreatedHeight < b.CreatedHeight || (a.CreatedHeight == b.CreatedHeight && a.ID > b.ID) {
			t.Fatalf("not newest first at %d: %+v then %+v", i, a, b)
		}
	}

	var seen []string
	cursor := ""
	for page := 0; ; page++ {
		path := "/api/v1/CELLS20/tokens?limit=2"
		if cursor != "" {
			path += "&cursor=" + url.QueryEscape(cursor)
		}
		var resp CELLS20TokensResponse
		r.getJSON(path, &resp)
		for _, tok := range resp.Tokens {
			seen = append(seen, tok.ID)
		}
		if page == 0 {
			// A new token arrives after the first page: it is newer than
			// everything, so it lands before the cursor and does not move
			// the next pages.
			r.height = 20
			r.send(alice, createAction("LATE", "1", "1", chain.CELLS20MintNone, ""))
		}
		if !resp.HasMore {
			if resp.NextCursor != "" {
				t.Fatal("last page carries a cursor")
			}
			break
		}
		if resp.NextCursor == "" || len(resp.Tokens) != 2 {
			t.Fatalf("page %d: %+v", page, resp)
		}
		cursor = resp.NextCursor
	}
	if len(seen) != 6 {
		t.Fatalf("paged %d tokens, want 6: %v", len(seen), seen)
	}
	for i, tok := range all.Tokens {
		if seen[i] != tok.ID {
			t.Fatalf("page order differs at %d", i)
		}
	}

	// Holders: distinct and equal balances.
	token := ids[0]
	var holders []CELLS20APIWallet
	for _, amount := range []string{"300", "100", "100", "50", "100"} {
		w := r.wallet(0)
		holders = append(holders, w)
		r.send(alice, chain.CELLS20Action{Action: chain.CELLS20ActionTransfer, Token: token, To: w.address, Amount: amount})
	}
	var full CELLS20HoldersResponse
	r.getJSON("/api/v1/CELLS20/tokens/"+token+"/holders", &full)
	// alice keeps 350; then 300, three of 100 (by address) and 50.
	if len(full.Holders) != 6 || full.Holders[0] != (chain.CELLS20Holder{Address: alice.address, Balance: "350"}) ||
		full.Holders[1] != (chain.CELLS20Holder{Address: holders[0].address, Balance: "300"}) ||
		full.Holders[5] != (chain.CELLS20Holder{Address: holders[3].address, Balance: "50"}) {
		t.Fatalf("holders: %+v", full.Holders)
	}
	var paged []chain.CELLS20Holder
	cursor = ""
	for {
		path := "/api/v1/CELLS20/tokens/" + token + "/holders?limit=2"
		if cursor != "" {
			path += "&cursor=" + url.QueryEscape(cursor)
		}
		var resp CELLS20HoldersResponse
		r.getJSON(path, &resp)
		paged = append(paged, resp.Holders...)
		if !resp.HasMore {
			break
		}
		cursor = resp.NextCursor
	}
	if len(paged) != len(full.Holders) {
		t.Fatalf("paged %d holders, want %d", len(paged), len(full.Holders))
	}
	for i := range paged {
		if paged[i] != full.Holders[i] {
			t.Fatalf("holder page order differs at %d: %+v vs %+v", i, paged[i], full.Holders[i])
		}
	}
	// A cursor past the end gives an empty last page.
	var tail CELLS20HoldersResponse
	r.getJSON("/api/v1/CELLS20/tokens/"+token+"/holders?cursor="+url.QueryEscape("0:"+strings.Repeat("f", 64)), &tail)
	if len(tail.Holders) != 0 || tail.HasMore {
		t.Fatalf("past the end: %+v", tail)
	}
}
