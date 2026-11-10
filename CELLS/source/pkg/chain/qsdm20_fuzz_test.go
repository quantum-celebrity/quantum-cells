package chain

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math/big"
	"reflect"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	"github.com/blackbeardONE/CELLS/pkg/mempool"
)

// Fuzz targets for the CELLS-20 payload decoder and transaction verifier,
// which gate a replicated token state on every node.

// CELLS20RefMaxUnits is 2^256 - 1, computed apart from the code under test.
var CELLS20RefMaxUnits = new(big.Int).Sub(new(big.Int).Exp(big.NewInt(2), big.NewInt(256), nil), big.NewInt(1))

// CELLS20RefUnits is a reference parser for the amount rule: digits only, no
// leading zero, 0 (when allowed) or 1 .. 2^256 - 1.
func CELLS20RefUnits(s string, allowZero bool) (*big.Int, bool) {
	if s == "" || strings.TrimLeft(s, "0123456789") != "" || (len(s) > 1 && s[0] == '0') {
		return nil, false
	}
	v, ok := new(big.Int).SetString(s, 10)
	if !ok || v.Cmp(CELLS20RefMaxUnits) > 0 || (v.Sign() == 0 && !allowZero) {
		return nil, false
	}
	return v, true
}

func CELLS20RefHex64(s string) bool {
	return len(s) == 64 && strings.Trim(s, "0123456789abcdef") == ""
}

// checkAcceptedCELLS20Action asserts the documented field rules for an action
// the decoder accepted, without the decoder's own helpers.
func checkAcceptedCELLS20Action(t *testing.T, a CELLS20Action) {
	t.Helper()
	fail := func(rule string) {
		t.Helper()
		t.Fatalf("accepted CELLS-20 action breaks the %s rule: %+v", rule, a)
	}
	createFields := a.Symbol != "" || a.Name != "" || a.Decimals != nil || a.MaxSupply != "" ||
		a.InitialSupply != "" || a.Mint != "" || a.MintLimit != ""
	switch a.Action {
	case CELLS20ActionCreate:
		if a.Token != "" || a.To != "" || a.Amount != "" {
			fail("create field set")
		}
		if len(a.Symbol) < 1 || len(a.Symbol) > 12 || strings.Trim(a.Symbol, "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789") != "" {
			fail("symbol")
		}
		if !utf8.ValidString(a.Name) || utf8.RuneCountInString(a.Name) < 1 || utf8.RuneCountInString(a.Name) > 48 {
			fail("name length")
		}
		for _, r := range a.Name {
			if unicode.Is(unicode.Cc, r) {
				fail("name control character")
			}
		}
		if a.Decimals == nil || *a.Decimals > 18 {
			fail("decimals")
		}
		maxSupply, ok := CELLS20RefUnits(a.MaxSupply, false)
		if !ok {
			fail("max_supply")
		}
		initial, ok := CELLS20RefUnits(a.InitialSupply, true)
		if !ok || initial.Cmp(maxSupply) > 0 {
			fail("initial_supply")
		}
		switch a.Mint {
		case CELLS20MintNone:
			if a.MintLimit != "" || initial.Cmp(maxSupply) != 0 {
				fail("mint none")
			}
		case CELLS20MintIssuer:
			if a.MintLimit != "" {
				fail("mint issuer")
			}
		case CELLS20MintOpen:
			if _, ok := CELLS20RefUnits(a.MintLimit, false); !ok {
				fail("mint open")
			}
		default:
			fail("mint mode")
		}
	case CELLS20ActionTransfer, CELLS20ActionMint, CELLS20ActionBurn, CELLS20ActionRenounce:
		if createFields {
			fail("non-create field set")
		}
		if !CELLS20RefHex64(a.Token) {
			fail("token id")
		}
		needsTo := a.Action == CELLS20ActionTransfer || a.Action == CELLS20ActionMint
		if needsTo != (a.To != "") || (needsTo && !CELLS20RefHex64(a.To)) {
			fail("to")
		}
		needsAmount := a.Action != CELLS20ActionRenounce
		if needsAmount != (a.Amount != "") {
			fail("amount presence")
		}
		if needsAmount {
			if _, ok := CELLS20RefUnits(a.Amount, false); !ok {
				fail("amount")
			}
		}
	default:
		fail("action set")
	}
}

func CELLS20FuzzSeedActions() []CELLS20Action {
	return []CELLS20Action{
		CELLS20Create("GROOT", "Groot Coin", 8, "1000000", "1000000", CELLS20MintNone, ""),
		CELLS20Create("FAIR21", "Fair launch é <&>", 0, "21000000", "0", CELLS20MintOpen, "1000"),
		CELLS20Create("ISS", "Issuer", 18, CELLS20MaxUnitsString, "0", CELLS20MintIssuer, ""),
		CELLS20Transfer(CELLS20HexA, CELLS20HexB, "1"),
		CELLS20MintAction(CELLS20HexA, CELLS20HexC, CELLS20MaxUnitsString),
		CELLS20Burn(CELLS20HexB, "77"),
		CELLS20Renounce(CELLS20HexC),
	}
}

func FuzzDecodeCELLS20Payload(f *testing.F) {
	for _, a := range CELLS20FuzzSeedActions() {
		raw, err := json.Marshal(a)
		if err != nil {
			f.Fatal(err)
		}
		if _, err := DecodeCELLS20Payload(raw); err != nil {
			f.Fatalf("genuine seed %s rejected: %v", raw, err)
		}
		f.Add(raw)
		f.Add(append(bytes.Clone(raw), ' '))
		f.Add(bytes.Replace(raw, []byte(`":"`), []byte(`": "`), 1))
	}
	for _, s := range []string{
		``, `null`, `{}`, `[]`, `{"action":"create"}`,
		`{"action":"transfer","token":"` + CELLS20HexA + `","to":"` + CELLS20HexB + `","amount":"01"}`,
		`{"action":"transfer","token":"` + CELLS20HexA + `","to":"` + CELLS20HexB + `","amount":"` + CELLS20TooManyUnits + `"}`,
		`{"action":"create","symbol":"A","name":"\u0007","decimals":0,"max_supply":"1","initial_supply":"1","mint":"none"}`,
		`{"action":"create","symbol":"A","name":"x","decimals":19,"max_supply":"1","initial_supply":"1","mint":"none"}`,
		`{"action":"create","symbol":"A","name":"x","decimals":0,"max_supply":"2","initial_supply":"1","mint":"none"}`,
		`{"action":"burn","token":"` + CELLS20HexA + `","to":"` + CELLS20HexB + `","amount":"1"}`,
		`{"Action":"renounce","token":"` + CELLS20HexA + `"}`,
		`{"action":"renounce","token":"` + CELLS20HexA + `","token":"` + CELLS20HexB + `"}`,
	} {
		f.Add([]byte(s))
	}

	f.Fuzz(func(t *testing.T, payload []byte) {
		orig := bytes.Clone(payload)
		a, err := DecodeCELLS20Payload(payload)
		if !bytes.Equal(payload, orig) {
			t.Fatal("DecodeCELLS20Payload modified its input")
		}
		if err != nil {
			if !errors.Is(err, ErrCELLS20Field) {
				t.Fatalf("refusal is not ErrCELLS20Field: %v", err)
			}
			return
		}
		canonical, err := json.Marshal(a)
		if err != nil || !bytes.Equal(canonical, payload) {
			t.Fatalf("accepted a non-canonical payload %q (canonical %q, %v)", payload, canonical, err)
		}
		checkAcceptedCELLS20Action(t, a)
		again, err := DecodeCELLS20Payload(canonical)
		if err != nil || !reflect.DeepEqual(again, a) {
			t.Fatalf("an accepted payload does not decode the same way twice: %v", err)
		}
	})
}

// FuzzVerifyCELLS20Tx builds a token transaction from its fields, signs it
// with a fixed key and checks the verifier: it accepts exactly the actions
// the field rules allow, and only signatures that cover the transaction.
func FuzzVerifyCELLS20Tx(f *testing.F) {
	key := newFuzzKey(20)
	add := func(a CELLS20Action, nonce, feeDust uint64, sender string) {
		decimals := int16(-1)
		if a.Decimals != nil {
			decimals = int16(*a.Decimals)
		}
		f.Add(a.Action, a.Symbol, a.Name, decimals, a.MaxSupply, a.InitialSupply, a.Mint, a.MintLimit,
			a.Token, a.To, a.Amount, nonce, feeDust, sender)
	}
	for i, a := range CELLS20FuzzSeedActions() {
		add(a, uint64(i), uint64(i)*1000, "")
	}
	add(CELLS20Transfer(CELLS20HexA, CELLS20HexB, "5"), 0, 0, CELLS20HexC)         // not the signing key
	add(CELLS20Transfer(CELLS20HexA, CELLS20HexB, "0"), 0, 0, "")                 // bad amount
	add(CELLS20Create("lower", "x", 0, "1", "1", CELLS20MintNone, ""), 0, 0, "") // bad symbol

	publicKey := hex.EncodeToString(key.pub)
	f.Fuzz(func(t *testing.T, action, symbol, name string, decimals int16, maxSupply, initial, mint, limit,
		token, to, amount string, nonce, feeDust uint64, sender string) {
		a := CELLS20Action{
			Action: action, Symbol: symbol, Name: name, MaxSupply: maxSupply, InitialSupply: initial,
			Mint: mint, MintLimit: limit, Token: token, To: to, Amount: amount,
		}
		if decimals >= 0 && decimals <= 255 {
			a.Decimals = CELLS20Decimals(uint8(decimals))
		}
		// The action as it arrives: through a JSON round trip.
		raw, err := json.Marshal(a)
		if err != nil {
			t.Fatal(err)
		}
		var wire CELLS20Action
		if err := json.Unmarshal(raw, &wire); err != nil {
			t.Fatal(err)
		}
		payload, err := json.Marshal(wire)
		if err != nil {
			t.Fatal(err)
		}
		if sender == "" {
			sender = key.address
		}
		fee, feeErr := CELLS20FeeCell(feeDust)
		if feeErr != nil {
			return
		}
		digest := CELLS20SigningDigest(sender, nonce, feeDust, wire)
		tx := &mempool.Tx{
			ID: CELLS20TxID(digest), Sender: sender, Fee: fee, Nonce: nonce, Payload: payload,
			ContractID: CELLS20ContractID, PublicKey: publicKey, Signature: hex.EncodeToString(key.mustSign(t, digest)),
		}
		orig := cloneTx(tx)
		got, err := VerifyCELLS20Tx(tx)
		if !reflect.DeepEqual(tx, orig) {
			t.Fatal("VerifyCELLS20Tx modified its input")
		}
		shapeErr := ValidateCELLS20Action(wire)
		switch {
		case shapeErr != nil:
			if !errors.Is(err, ErrCELLS20Field) {
				t.Fatalf("malformed action (%v) gave %v", shapeErr, err)
			}
			return
		case sender != key.address:
			if err == nil {
				t.Fatalf("accepted sender %q, which is not the signing key", sender)
			}
			return
		case err != nil:
			t.Fatalf("rejected a well-formed action signed by its sender: %v", err)
		}
		if !reflect.DeepEqual(got, wire) {
			t.Fatal("verified action differs from the signed one")
		}
		checkAcceptedCELLS20Action(t, got)
		if !fuzzVerifyMLDSA(key.pub, digest, mustHex(t, tx.Signature)) {
			t.Fatal("accepted a signature the reference ML-DSA-87 verifier rejects")
		}
		if tx.ID != CELLS20TxIDPrefix+hex.EncodeToString(digest) {
			t.Fatal("tx id is not bound to the digest")
		}
		// The signature does not carry over to another nonce or fee, even
		// with the id fixed up.
		for _, change := range []func(*mempool.Tx){
			func(m *mempool.Tx) {
				m.Nonce++
				m.ID = CELLS20TxID(CELLS20SigningDigest(sender, m.Nonce, feeDust, wire))
			},
			func(m *mempool.Tx) {
				fee2, err := CELLS20FeeCell(feeDust + 1)
				if err != nil {
					m.Fee = -1
					return
				}
				m.Fee = fee2
				m.ID = CELLS20TxID(CELLS20SigningDigest(sender, nonce, feeDust+1, wire))
			},
		} {
			moved := cloneTx(tx)
			change(moved)
			if _, err := VerifyCELLS20Tx(moved); err == nil {
				t.Fatal("a signature verified for a different nonce or fee")
			}
		}
	})
}

func mustHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
