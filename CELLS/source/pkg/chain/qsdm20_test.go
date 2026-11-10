package chain

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/blackbeardONE/CELLS/pkg/mempool"
	"github.com/cloudflare/circl/sign/mldsa/mldsa87"
)

// Shared CELLS-20 test helpers (also used by CELLS20_state_test.go,
// CELLS20_replay_test.go and CELLS20_fuzz_test.go).

func setCELLS20ActivationForTest(t testing.TB, h uint64) {
	t.Helper()
	prev := CELLS20ActivationHeight()
	SetCELLS20ActivationHeight(h)
	t.Cleanup(func() { SetCELLS20ActivationHeight(prev) })
}

type CELLS20Wallet struct {
	private *mldsa87.PrivateKey
	pub     []byte
	address string
}

func newCELLS20Wallet(t testing.TB) CELLS20Wallet {
	t.Helper()
	public, private, err := mldsa87.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	pub, err := public.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	return CELLS20Wallet{private: private, pub: pub, address: BFTValidatorAddress(pub)}
}

// CELLS20UnsignedTx builds the transaction exactly as the submit API does,
// with no signature.
func CELLS20UnsignedTx(t testing.TB, sender string, nonce, feeDust uint64, a CELLS20Action) *mempool.Tx {
	t.Helper()
	payload, err := json.Marshal(a)
	if err != nil {
		t.Fatal(err)
	}
	fee, err := CELLS20FeeCell(feeDust)
	if err != nil {
		t.Fatal(err)
	}
	return &mempool.Tx{
		ID:         CELLS20TxID(CELLS20SigningDigest(sender, nonce, feeDust, a)),
		Sender:     sender,
		Fee:        fee,
		Nonce:      nonce,
		Payload:    payload,
		ContractID: CELLS20ContractID,
	}
}

// signedTx builds and signs a token transaction from w.
func (w CELLS20Wallet) signedTx(t testing.TB, nonce, feeDust uint64, a CELLS20Action) *mempool.Tx {
	t.Helper()
	tx := CELLS20UnsignedTx(t, w.address, nonce, feeDust, a)
	sig := make([]byte, mldsa87.SignatureSize)
	if err := mldsa87.SignTo(w.private, CELLS20SigningDigest(w.address, nonce, feeDust, a), nil, true, sig); err != nil {
		t.Fatal(err)
	}
	tx.Signature = hex.EncodeToString(sig)
	tx.PublicKey = hex.EncodeToString(w.pub)
	return tx
}

func CELLS20Create(symbol, name string, decimals uint8, maxSupply, initial, mint, limit string) CELLS20Action {
	return CELLS20Action{
		Action: CELLS20ActionCreate, Symbol: symbol, Name: name, Decimals: CELLS20Decimals(decimals),
		MaxSupply: maxSupply, InitialSupply: initial, Mint: mint, MintLimit: limit,
	}
}

func CELLS20Transfer(token, to, amount string) CELLS20Action {
	return CELLS20Action{Action: CELLS20ActionTransfer, Token: token, To: to, Amount: amount}
}

func CELLS20MintAction(token, to, amount string) CELLS20Action {
	return CELLS20Action{Action: CELLS20ActionMint, Token: token, To: to, Amount: amount}
}

func CELLS20Burn(token, amount string) CELLS20Action {
	return CELLS20Action{Action: CELLS20ActionBurn, Token: token, Amount: amount}
}

func CELLS20Renounce(token string) CELLS20Action {
	return CELLS20Action{Action: CELLS20ActionRenounce, Token: token}
}

var (
	CELLS20MaxUnitsString = "115792089237316195423570985008687907853269984665640564039457584007913129639935"
	CELLS20TooManyUnits   = "115792089237316195423570985008687907853269984665640564039457584007913129639936"
	CELLS20HexA           = strings.Repeat("ab", 32)
	CELLS20HexB           = strings.Repeat("cd", 32)
	CELLS20HexC           = strings.Repeat("ef", 32)
)

func TestCELLS20ActivationHeight(t *testing.T) {
	setCELLS20ActivationForTest(t, 0)
	if CELLS20ActiveAt(0) || CELLS20ActiveAt(1) || CELLS20ActiveAt(math.MaxUint64) {
		t.Fatal("activation height 0 must never activate")
	}
	if err := checkCELLS20Active(1 << 40); !errors.Is(err, ErrCELLS20Inactive) {
		t.Fatalf("switched off: %v", err)
	}
	SetCELLS20ActivationHeight(100)
	if CELLS20ActiveAt(99) || !CELLS20ActiveAt(100) || !CELLS20ActiveAt(101) || CELLS20ActiveAt(0) {
		t.Fatal("the rule must govern exactly heights >= 100")
	}
	if CELLS20ActivationHeight() != 100 {
		t.Fatalf("height = %d", CELLS20ActivationHeight())
	}
}

// The token id is the lowercase hex SHA-256 of the length-prefixed domain,
// creator and nonce (CELLS_20.md, "Token id").
func TestCELLS20TokenIDDerivation(t *testing.T) {
	creator := CELLS20HexA
	preimage := "|domain:16:CELLS/token/v1/id|creator:64:" + creator + "|nonce:1:7"
	sum := sha256.Sum256([]byte(preimage))
	if got, want := CELLS20TokenID(creator, 7), hex.EncodeToString(sum[:]); got != want {
		t.Fatalf("token id = %s, want %s", got, want)
	}
	// The vector published in CELLS_20.md.
	if got := CELLS20TokenID(creator, 7); got != "78d65edcdf5af01c85401a31e3ffd6a4e19e90c16a62f3a222650ef8497b768a" {
		t.Fatalf("token id vector = %s", got)
	}
	if CELLS20TokenID(creator, 7) == CELLS20TokenID(creator, 8) ||
		CELLS20TokenID(creator, 7) == CELLS20TokenID(CELLS20HexB, 7) {
		t.Fatal("ids must differ by creator and by nonce")
	}
	// Length prefixes keep "creator 1, nonce 23" apart from "creator 12, nonce 3".
	if CELLS20TokenID("1", 23) == CELLS20TokenID("12", 3) {
		t.Fatal("length prefixes do not separate the fields")
	}
	if id := CELLS20TokenID(creator, 0); !validCELLS20Hex64(id) {
		t.Fatalf("id %q is not 64 lowercase hex", id)
	}
}

// The signed message is the SHA-256 of the length-prefixed domain, sender,
// nonce, fee in dust, action and the action's fields in table order.
func TestCELLS20SigningPreimageVector(t *testing.T) {
	transfer := CELLS20Transfer(CELLS20HexB, CELLS20HexC, "1000")
	want := "|domain:16:CELLS/token/v1/tx|sender:64:" + CELLS20HexA + "|nonce:1:7|fee_dust:4:1500" +
		"|action:8:transfer|token:64:" + CELLS20HexB + "|to:64:" + CELLS20HexC + "|amount:4:1000"
	if got := CELLS20SigningPreimage(CELLS20HexA, 7, 1500, transfer); got != want {
		t.Fatalf("transfer preimage\n got %s\nwant %s", got, want)
	}
	sum := sha256.Sum256([]byte(want))
	if got := CELLS20SigningDigest(CELLS20HexA, 7, 1500, transfer); hex.EncodeToString(got) != hex.EncodeToString(sum[:]) {
		t.Fatal("digest is not the SHA-256 of the preimage")
	}
	// The vectors published in CELLS_20.md.
	if got := hex.EncodeToString(sum[:]); got != "a38b5c1cba8af0190de2c87489608e2a2977d227626ef91117a1a1797db5b648" {
		t.Fatalf("transfer digest vector = %s", got)
	}
	if got := hex.EncodeToString(CELLS20SigningDigest(CELLS20HexA, 0, 0,
		CELLS20Create("GROOT", "Groot Coin", 8, "1000000", "250", CELLS20MintIssuer, ""))); got != "d8edcbaca25b19b26a51c2c40fd51c0712d68b5ca5422289476334015870811d" {
		t.Fatalf("create digest vector = %s", got)
	}
	if got := CELLS20TxID(sum[:]); got != "CELLS20-"+hex.EncodeToString(sum[:]) {
		t.Fatalf("tx id = %s", got)
	}

	create := CELLS20Create("GROOT", "Groot Coin", 8, "1000000", "250", CELLS20MintIssuer, "")
	want = "|domain:16:CELLS/token/v1/tx|sender:64:" + CELLS20HexA + "|nonce:1:0|fee_dust:1:0" +
		"|action:6:create|symbol:5:GROOT|name:10:Groot Coin|decimals:1:8|max_supply:7:1000000" +
		"|initial_supply:3:250|mint:6:issuer|mint_limit:0:"
	if got := CELLS20SigningPreimage(CELLS20HexA, 0, 0, create); got != want {
		t.Fatalf("create preimage\n got %s\nwant %s", got, want)
	}
	burn := CELLS20Burn(CELLS20HexB, "5")
	want = "|domain:16:CELLS/token/v1/tx|sender:64:" + CELLS20HexA + "|nonce:2:12|fee_dust:3:100" +
		"|action:4:burn|token:64:" + CELLS20HexB + "|amount:1:5"
	if got := CELLS20SigningPreimage(CELLS20HexA, 12, 100, burn); got != want {
		t.Fatalf("burn preimage\n got %s\nwant %s", got, want)
	}
	renounce := CELLS20Renounce(CELLS20HexB)
	want = "|domain:16:CELLS/token/v1/tx|sender:64:" + CELLS20HexA + "|nonce:1:3|fee_dust:1:0" +
		"|action:8:renounce|token:64:" + CELLS20HexB
	if got := CELLS20SigningPreimage(CELLS20HexA, 3, 0, renounce); got != want {
		t.Fatalf("renounce preimage\n got %s\nwant %s", got, want)
	}
	// A name byte inside a field cannot shift into the next field: the
	// length is written first. "|" in the name is just a byte.
	tricky := CELLS20Create("A", "x|symbol:1:B", 0, "1", "1", CELLS20MintNone, "")
	if strings.Count(CELLS20SigningPreimage(CELLS20HexA, 0, 0, tricky), "|name:12:x|symbol:1:B|") != 1 {
		t.Fatal("name is not written with its byte length")
	}
}

// Every field the signature covers changes the digest: action, sender,
// nonce, fee and each action field.
func TestCELLS20DigestCoversEveryField(t *testing.T) {
	base := CELLS20Transfer(CELLS20HexB, CELLS20HexC, "10")
	d0 := hex.EncodeToString(CELLS20SigningDigest(CELLS20HexA, 1, 2, base))
	changed := map[string]string{}
	add := func(name string, d []byte) { changed[name] = hex.EncodeToString(d) }
	mint := base
	mint.Action = CELLS20ActionMint
	add("action", CELLS20SigningDigest(CELLS20HexA, 1, 2, mint))
	add("sender", CELLS20SigningDigest(CELLS20HexC, 1, 2, base))
	add("nonce", CELLS20SigningDigest(CELLS20HexA, 2, 2, base))
	add("fee", CELLS20SigningDigest(CELLS20HexA, 1, 3, base))
	other := base
	other.Token = CELLS20HexA
	add("token", CELLS20SigningDigest(CELLS20HexA, 1, 2, other))
	other = base
	other.To = CELLS20HexA
	add("to", CELLS20SigningDigest(CELLS20HexA, 1, 2, other))
	other = base
	other.Amount = "11"
	add("amount", CELLS20SigningDigest(CELLS20HexA, 1, 2, other))
	seen := map[string]string{d0: "base"}
	for name, d := range changed {
		if prev, dup := seen[d]; dup {
			t.Fatalf("changing %s gives the same digest as %s", name, prev)
		}
		seen[d] = name
	}
}

func TestCELLS20FieldRules(t *testing.T) {
	ok := func(a CELLS20Action) CELLS20Action { return a }
	valid := []CELLS20Action{
		CELLS20Create("A", "x", 0, "1", "1", CELLS20MintNone, ""),
		CELLS20Create("ABCDEFGHIJ12", strings.Repeat("é", 48), 18, CELLS20MaxUnitsString, CELLS20MaxUnitsString, CELLS20MintNone, ""),
		CELLS20Create("Q20", "Fair launch", 6, "21000000", "0", CELLS20MintOpen, "1000"),
		CELLS20Create("ISS", "Issuer coin", 2, "500", "0", CELLS20MintIssuer, ""),
		CELLS20Create("ISS", "Issuer coin", 2, "500", "500", CELLS20MintIssuer, ""),
		CELLS20Create("OPEN", "Big limit", 0, "10", "3", CELLS20MintOpen, CELLS20MaxUnitsString),
		CELLS20Transfer(CELLS20HexA, CELLS20HexB, "1"),
		CELLS20Transfer(CELLS20HexA, CELLS20HexB, CELLS20MaxUnitsString),
		CELLS20MintAction(CELLS20HexA, CELLS20HexB, "7"),
		CELLS20Burn(CELLS20HexA, "9"),
		CELLS20Renounce(CELLS20HexA),
	}
	for i, a := range valid {
		if err := ValidateCELLS20Action(ok(a)); err != nil {
			t.Fatalf("valid[%d] %+v refused: %v", i, a, err)
		}
	}

	create := func(mutate func(*CELLS20Action)) CELLS20Action {
		a := CELLS20Create("ABC", "Name", 8, "100", "100", CELLS20MintNone, "")
		mutate(&a)
		return a
	}
	invalid := map[string]CELLS20Action{
		"symbol empty":             create(func(a *CELLS20Action) { a.Symbol = "" }),
		"symbol 13 characters":     create(func(a *CELLS20Action) { a.Symbol = "ABCDEFGHIJ123" }),
		"symbol lower case":        create(func(a *CELLS20Action) { a.Symbol = "abc" }),
		"symbol punctuation":       create(func(a *CELLS20Action) { a.Symbol = "AB-C" }),
		"symbol space":             create(func(a *CELLS20Action) { a.Symbol = "AB C" }),
		"symbol non-ASCII":         create(func(a *CELLS20Action) { a.Symbol = "ÄBC" }),
		"name empty":               create(func(a *CELLS20Action) { a.Name = "" }),
		"name 49 characters":       create(func(a *CELLS20Action) { a.Name = strings.Repeat("n", 49) }),
		"name 49 multibyte":        create(func(a *CELLS20Action) { a.Name = strings.Repeat("é", 49) }),
		"name control BEL":         create(func(a *CELLS20Action) { a.Name = "bad\x07name" }),
		"name control newline":     create(func(a *CELLS20Action) { a.Name = "two\nlines" }),
		"name control DEL":         create(func(a *CELLS20Action) { a.Name = "del\x7f" }),
		"name control C1":          create(func(a *CELLS20Action) { a.Name = "c1\u0085" }),
		"name invalid UTF-8":       create(func(a *CELLS20Action) { a.Name = "bad\xff" }),
		"decimals missing":         create(func(a *CELLS20Action) { a.Decimals = nil }),
		"decimals 19":              create(func(a *CELLS20Action) { a.Decimals = CELLS20Decimals(19) }),
		"max_supply missing":       create(func(a *CELLS20Action) { a.MaxSupply = "" }),
		"max_supply zero":          create(func(a *CELLS20Action) { a.MaxSupply, a.InitialSupply = "0", "0" }),
		"max_supply leading zero":  create(func(a *CELLS20Action) { a.MaxSupply = "0100" }),
		"max_supply negative":      create(func(a *CELLS20Action) { a.MaxSupply = "-100" }),
		"max_supply plus sign":     create(func(a *CELLS20Action) { a.MaxSupply = "+100" }),
		"max_supply decimal point": create(func(a *CELLS20Action) { a.MaxSupply = "100.0" }),
		"max_supply exponent":      create(func(a *CELLS20Action) { a.MaxSupply = "1e2" }),
		"max_supply space":         create(func(a *CELLS20Action) { a.MaxSupply = " 100" }),
		"max_supply underscore":    create(func(a *CELLS20Action) { a.MaxSupply = "1_00" }),
		"max_supply hex":           create(func(a *CELLS20Action) { a.MaxSupply = "0x64" }),
		"max_supply over 2^256-1": create(func(a *CELLS20Action) {
			a.MaxSupply, a.InitialSupply = CELLS20TooManyUnits, CELLS20TooManyUnits
		}),
		"max_supply 79 digits":      create(func(a *CELLS20Action) { a.MaxSupply = "1" + strings.Repeat("0", 78) }),
		"initial_supply missing":    create(func(a *CELLS20Action) { a.InitialSupply = "" }),
		"initial over max":          create(func(a *CELLS20Action) { a.Mint, a.InitialSupply = CELLS20MintIssuer, "101" }),
		"none initial below max":    create(func(a *CELLS20Action) { a.InitialSupply = "99" }),
		"none initial zero":         create(func(a *CELLS20Action) { a.InitialSupply = "0" }),
		"none with mint_limit":      create(func(a *CELLS20Action) { a.MintLimit = "5" }),
		"issuer with mint_limit":    create(func(a *CELLS20Action) { a.Mint, a.MintLimit = CELLS20MintIssuer, "5" }),
		"open without mint_limit":   create(func(a *CELLS20Action) { a.Mint, a.InitialSupply = CELLS20MintOpen, "0" }),
		"open mint_limit zero":      create(func(a *CELLS20Action) { a.Mint, a.InitialSupply, a.MintLimit = CELLS20MintOpen, "0", "0" }),
		"open mint_limit malformed": create(func(a *CELLS20Action) { a.Mint, a.InitialSupply, a.MintLimit = CELLS20MintOpen, "0", "01" }),
		"mint mode unknown":         create(func(a *CELLS20Action) { a.Mint = "anyone" }),
		"mint mode empty":           create(func(a *CELLS20Action) { a.Mint = "" }),
		"mint mode upper case":      create(func(a *CELLS20Action) { a.Mint = "NONE" }),
		"create with token":         create(func(a *CELLS20Action) { a.Token = CELLS20HexA }),
		"create with to":            create(func(a *CELLS20Action) { a.To = CELLS20HexA }),
		"create with amount":        create(func(a *CELLS20Action) { a.Amount = "1" }),
		"action unknown":            {Action: "approve", Token: CELLS20HexA},
		"action empty":              {},
		"action upper case":         {Action: "Transfer", Token: CELLS20HexA, To: CELLS20HexB, Amount: "1"},
		"transfer amount zero":      CELLS20Transfer(CELLS20HexA, CELLS20HexB, "0"),
		"transfer amount missing":   CELLS20Transfer(CELLS20HexA, CELLS20HexB, ""),
		"transfer amount over":      CELLS20Transfer(CELLS20HexA, CELLS20HexB, CELLS20TooManyUnits),
		"transfer amount leading 0": CELLS20Transfer(CELLS20HexA, CELLS20HexB, "007"),
		"transfer token upper":      CELLS20Transfer(strings.ToUpper(CELLS20HexA), CELLS20HexB, "1"),
		"transfer token short":      CELLS20Transfer(CELLS20HexA[:62], CELLS20HexB, "1"),
		"transfer token missing":    CELLS20Transfer("", CELLS20HexB, "1"),
		"transfer to upper":         CELLS20Transfer(CELLS20HexA, strings.ToUpper(CELLS20HexB), "1"),
		"transfer to not hex":       CELLS20Transfer(CELLS20HexA, strings.Repeat("zz", 32), "1"),
		"transfer to missing":       CELLS20Transfer(CELLS20HexA, "", "1"),
		"transfer with symbol": func() CELLS20Action {
			a := CELLS20Transfer(CELLS20HexA, CELLS20HexB, "1")
			a.Symbol = "X"
			return a
		}(),
		"transfer with decimals": func() CELLS20Action {
			a := CELLS20Transfer(CELLS20HexA, CELLS20HexB, "1")
			a.Decimals = CELLS20Decimals(0)
			return a
		}(),
		"mint to missing":       CELLS20MintAction(CELLS20HexA, "", "1"),
		"mint amount zero":      CELLS20MintAction(CELLS20HexA, CELLS20HexB, "0"),
		"burn with to":          {Action: CELLS20ActionBurn, Token: CELLS20HexA, To: CELLS20HexB, Amount: "1"},
		"burn amount zero":      CELLS20Burn(CELLS20HexA, "0"),
		"burn token bad":        CELLS20Burn("x", "1"),
		"renounce with amount":  {Action: CELLS20ActionRenounce, Token: CELLS20HexA, Amount: "1"},
		"renounce with to":      {Action: CELLS20ActionRenounce, Token: CELLS20HexA, To: CELLS20HexB},
		"renounce with mint":    {Action: CELLS20ActionRenounce, Token: CELLS20HexA, Mint: CELLS20MintNone},
		"renounce token absent": CELLS20Renounce(""),
	}
	for name, a := range invalid {
		if err := ValidateCELLS20Action(a); !errors.Is(err, ErrCELLS20Field) {
			t.Errorf("%s: %+v accepted or wrong error: %v", name, a, err)
		}
	}
}

// One action has one payload. Anything else is refused: spaces, another key
// order, unknown, duplicate, null or empty fields, other key spellings.
func TestCELLS20PayloadMustBeCanonical(t *testing.T) {
	a := CELLS20Transfer(CELLS20HexA, CELLS20HexB, "5")
	canonical, _ := json.Marshal(a)
	if got, err := DecodeCELLS20Payload(canonical); err != nil || got.Amount != "5" {
		t.Fatalf("canonical payload refused: %v", err)
	}
	if string(canonical) != `{"action":"transfer","token":"`+CELLS20HexA+`","to":"`+CELLS20HexB+`","amount":"5"}` {
		t.Fatalf("canonical form changed: %s", canonical)
	}
	// encoding/json writes &, < and > as backslash-u escapes.
	bs := "\\"
	create, _ := json.Marshal(CELLS20Create("ABC", "Tom & <Jerry>", 0, "9", "9", CELLS20MintNone, ""))
	wantCreate := `{"action":"create","symbol":"ABC","name":"Tom ` + bs + `u0026 ` + bs + `u003cJerry` + bs +
		`u003e","decimals":0,"max_supply":"9","initial_supply":"9","mint":"none"}`
	if string(create) != wantCreate {
		t.Fatalf("canonical create changed: %s", create)
	}
	if _, err := DecodeCELLS20Payload(create); err != nil {
		t.Fatalf("canonical create refused: %v", err)
	}
	tok, to := CELLS20HexA, CELLS20HexB
	bad := []string{
		``,
		`null`,
		`[]`,
		`"transfer"`,
		`{}`,
		` ` + string(canonical),
		string(canonical) + ` `,
		string(canonical) + "\n",
		string(canonical) + `{}`,
		`{"action": "transfer","token":"` + tok + `","to":"` + to + `","amount":"5"}`,
		`{"token":"` + tok + `","action":"transfer","to":"` + to + `","amount":"5"}`,
		`{"action":"transfer","token":"` + tok + `","to":"` + to + `","amount":"5","memo":"x"}`,
		`{"action":"transfer","action":"transfer","token":"` + tok + `","to":"` + to + `","amount":"5"}`,
		`{"Action":"transfer","token":"` + tok + `","to":"` + to + `","amount":"5"}`,
		`{"action":"transfer","token":"` + tok + `","to":"` + to + `","amount":5}`,
		`{"action":"transfer","symbol":"","token":"` + tok + `","to":"` + to + `","amount":"5"}`,
		`{"action":"transfer","decimals":null,"token":"` + tok + `","to":"` + to + `","amount":"5"}`,
		`{"action":"transfer","token":"` + tok + `","to":"` + to + `","amount":"` + bs + `u0035"}`,
		`{"action":"create","symbol":"ABC","name":"N","decimals":8.0,"max_supply":"9","initial_supply":"9","mint":"none"}`,
		`{"action":"create","symbol":"ABC","name":"N","decimals":"8","max_supply":"9","initial_supply":"9","mint":"none"}`,
		`{"action":"create","symbol":"ABC","name":"N","decimals":-1,"max_supply":"9","initial_supply":"9","mint":"none"}`,
		`{"action":"create","symbol":"ABC","name":"N","decimals":256,"max_supply":"9","initial_supply":"9","mint":"none"}`,
		`{"action":"create","symbol":"ABC","name":"N","decimals":1e1,"max_supply":"9","initial_supply":"9","mint":"none"}`,
		`{"action":"create","symbol":"ABC","name":"Tom & Jerry","decimals":0,"max_supply":"9","initial_supply":"9","mint":"none"}`,
		"{\"action\":\"create\",\"symbol\":\"ABC\",\"name\":\"bad\xff\",\"decimals\":0,\"max_supply\":\"9\",\"initial_supply\":\"9\",\"mint\":\"none\"}",
	}
	for _, p := range bad {
		if _, err := DecodeCELLS20Payload([]byte(p)); !errors.Is(err, ErrCELLS20Field) {
			t.Errorf("payload %q accepted or wrong error: %v", p, err)
		}
	}
}

func TestCELLS20FeeEncoding(t *testing.T) {
	for _, dust := range []uint64{0, 1, 3, 99, 100_000_000, 123_456_789, 9_000_000_000_000_000} {
		fee, err := CELLS20FeeCell(dust)
		if err != nil {
			t.Fatalf("fee %d dust: %v", dust, err)
		}
		back, err := CELLS20FeeDust(fee)
		if err != nil || back != dust {
			t.Fatalf("fee %d dust round trip = %d, %v", dust, back, err)
		}
	}
	for _, fee := range []float64{math.Copysign(0, -1), -1, math.NaN(), math.Inf(1), 0.123456789, 1e-9, math.Nextafter(0.1, 1)} {
		if _, err := CELLS20FeeDust(fee); !errors.Is(err, ErrCELLS20Field) {
			t.Errorf("fee %v accepted or wrong error: %v", fee, err)
		}
	}
	if _, err := CELLS20FeeCell(math.MaxUint64); err == nil {
		t.Fatal("a fee with no exact CELL value was accepted")
	}
}

func TestCELLS20TransactionShape(t *testing.T) {
	w := newCELLS20Wallet(t)
	good := w.signedTx(t, 3, 250, CELLS20Transfer(CELLS20HexA, CELLS20HexB, "10"))
	if _, err := VerifyCELLS20Tx(good); err != nil {
		t.Fatalf("well-formed transaction refused: %v", err)
	}
	mutate := func(f func(*mempool.Tx)) *mempool.Tx {
		tx := cloneTx(good)
		f(tx)
		return tx
	}
	cases := map[string]struct {
		tx   *mempool.Tx
		want error
	}{
		"wrong contract":     {mutate(func(tx *mempool.Tx) { tx.ContractID = StreamContractID }), ErrNotCELLS20Tx},
		"no contract":        {mutate(func(tx *mempool.Tx) { tx.ContractID = "" }), ErrNotCELLS20Tx},
		"CELL amount":        {mutate(func(tx *mempool.Tx) { tx.Amount = 1 }), ErrCELLS20Field},
		"negative zero":      {mutate(func(tx *mempool.Tx) { tx.Amount = math.Copysign(0, -1) }), ErrCELLS20Field},
		"recipient":          {mutate(func(tx *mempool.Tx) { tx.Recipient = CELLS20HexB }), ErrCELLS20Field},
		"gas limit":          {mutate(func(tx *mempool.Tx) { tx.GasLimit = 1 }), ErrCELLS20Field},
		"fee not dust":       {mutate(func(tx *mempool.Tx) { tx.Fee = math.Nextafter(tx.Fee, 1) }), ErrCELLS20Field},
		"fee changed":        {mutate(func(tx *mempool.Tx) { tx.Fee = DustToCell(251) }), ErrCELLS20Field},
		"negative fee":       {mutate(func(tx *mempool.Tx) { tx.Fee = -DustToCell(250) }), ErrCELLS20Field},
		"nonce changed":      {mutate(func(tx *mempool.Tx) { tx.Nonce = 4 }), ErrCELLS20Field},
		"id changed":         {mutate(func(tx *mempool.Tx) { tx.ID = "CELLS20-" + strings.Repeat("0", 64) }), ErrCELLS20Field},
		"id empty":           {mutate(func(tx *mempool.Tx) { tx.ID = "" }), ErrCELLS20Field},
		"sender upper case":  {mutate(func(tx *mempool.Tx) { tx.Sender = strings.ToUpper(tx.Sender) }), ErrCELLS20Field},
		"sender empty":       {mutate(func(tx *mempool.Tx) { tx.Sender = "" }), ErrCELLS20Field},
		"payload empty":      {mutate(func(tx *mempool.Tx) { tx.Payload = nil }), ErrCELLS20Field},
		"payload not canon":  {mutate(func(tx *mempool.Tx) { tx.Payload = append(tx.Payload, ' ') }), ErrCELLS20Field},
		"signature missing":  {mutate(func(tx *mempool.Tx) { tx.Signature = "" }), ErrCELLS20Signature},
		"public key missing": {mutate(func(tx *mempool.Tx) { tx.PublicKey = "" }), ErrCELLS20Signature},
		"signature upper":    {mutate(func(tx *mempool.Tx) { tx.Signature = strings.ToUpper(tx.Signature) }), ErrCELLS20Signature},
		"public key spaced":  {mutate(func(tx *mempool.Tx) { tx.PublicKey = " " + tx.PublicKey }), ErrCELLS20Signature},
		"signature short":    {mutate(func(tx *mempool.Tx) { tx.Signature = tx.Signature[:len(tx.Signature)-2] }), ErrCELLS20Signature},
	}
	for name, c := range cases {
		if _, err := VerifyCELLS20Tx(c.tx); !errors.Is(err, c.want) {
			t.Errorf("%s: got %v, want %v", name, err, c.want)
		}
	}
	if _, err := VerifyCELLS20Tx(nil); err == nil {
		t.Fatal("nil transaction accepted")
	}
}

func TestCELLS20SignatureChecks(t *testing.T) {
	w := newCELLS20Wallet(t)
	other := newCELLS20Wallet(t)
	good := w.signedTx(t, 0, 0, CELLS20Burn(CELLS20HexA, "3"))
	if _, err := VerifyCELLS20Tx(good); err != nil {
		t.Fatalf("valid signature refused: %v", err)
	}

	tampered := cloneTx(good)
	sig, _ := hex.DecodeString(tampered.Signature)
	sig[100] ^= 0x01
	tampered.Signature = hex.EncodeToString(sig)
	if _, err := VerifyCELLS20Tx(tampered); !errors.Is(err, ErrCELLS20Signature) {
		t.Fatalf("tampered signature: %v", err)
	}

	// Another wallet's key, with that wallet's own valid signature over the
	// same digest: the key does not hash to the sender.
	wrongKey := cloneTx(good)
	otherSig := make([]byte, mldsa87.SignatureSize)
	digest := CELLS20SigningDigest(w.address, 0, 0, CELLS20Burn(CELLS20HexA, "3"))
	if err := mldsa87.SignTo(other.private, digest, nil, true, otherSig); err != nil {
		t.Fatal(err)
	}
	wrongKey.PublicKey = hex.EncodeToString(other.pub)
	wrongKey.Signature = hex.EncodeToString(otherSig)
	if _, err := VerifyCELLS20Tx(wrongKey); !errors.Is(err, ErrCELLS20Signature) ||
		!strings.Contains(err.Error(), "does not hash to the sender") {
		t.Fatalf("wrong public key for the sender: %v", err)
	}

	// A changed amount with the id recomputed still fails: the signature
	// covers the fields, not the id.
	changed := cloneTx(good)
	a := CELLS20Burn(CELLS20HexA, "4")
	changed.Payload, _ = json.Marshal(a)
	changed.ID = CELLS20TxID(CELLS20SigningDigest(w.address, 0, 0, a))
	if _, err := VerifyCELLS20Tx(changed); !errors.Is(err, ErrCELLS20Signature) {
		t.Fatalf("changed amount with a valid id: %v", err)
	}

	// The same signature does not verify for another sender's transaction.
	moved := w.signedTx(t, 1, 0, CELLS20Burn(CELLS20HexA, "3"))
	moved.Signature = good.Signature
	if _, err := VerifyCELLS20Tx(moved); !errors.Is(err, ErrCELLS20Signature) {
		t.Fatalf("signature reused at another nonce: %v", err)
	}
}

func TestCELLS20AdmissionChecker(t *testing.T) {
	w := newCELLS20Wallet(t)
	tx := w.signedTx(t, 0, 0, CELLS20Create("ADM", "Admission", 0, "1", "1", CELLS20MintNone, ""))
	var calls int
	next := func(*mempool.Tx) error { calls++; return nil }
	height := uint64(99)
	source := func() (uint64, bool) { return height, true }
	check := CELLS20AdmissionChecker(source, next)

	setCELLS20ActivationForTest(t, 0)
	if err := check(tx); !errors.Is(err, ErrCELLS20Inactive) {
		t.Fatalf("switched off: %v", err)
	}
	if err := CELLS20AdmissionChecker(nil, next)(tx); !errors.Is(err, ErrCELLS20Inactive) {
		t.Fatalf("switched off without a height source: %v", err)
	}
	SetCELLS20ActivationHeight(100)
	if err := check(tx); !errors.Is(err, ErrCELLS20Inactive) {
		t.Fatalf("next block 99 is below 100: %v", err)
	}
	height = 100
	if err := check(tx); err != nil {
		t.Fatalf("next block 100: %v", err)
	}
	if err := CELLS20AdmissionChecker(nil, next)(tx); !errors.Is(err, ErrEnrollmentHeightUnset) {
		t.Fatalf("no height source must fail closed: %v", err)
	}
	if err := CELLS20AdmissionChecker(func() (uint64, bool) { return 0, false }, next)(tx); !errors.Is(err, ErrEnrollmentHeightUnset) {
		t.Fatalf("unset height source must fail closed: %v", err)
	}
	bad := cloneTx(tx)
	bad.Signature = strings.Repeat("00", mldsa87.SignatureSize)
	if err := check(bad); !errors.Is(err, ErrCELLS20Signature) {
		t.Fatalf("bad signature admitted: %v", err)
	}
	if calls != 0 {
		t.Fatal("token transactions must not reach the next layer")
	}
	if err := check(&mempool.Tx{ID: "plain", Sender: "a", Recipient: "b", Amount: 1}); err != nil || calls != 1 {
		t.Fatalf("other transactions go to the next layer: %v (calls %d)", err, calls)
	}
	if err := check(nil); err == nil {
		t.Fatal("nil transaction admitted")
	}
}
