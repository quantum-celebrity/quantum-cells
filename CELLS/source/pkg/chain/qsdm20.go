package chain

// CELLS20.go: CELLS-20 native tokens, version 1 (contract CELLS/token/v1).
//
// The rules are in CELLS/docs/docs/CELLS_20.md. This file holds the stateless
// half: the transaction shape, the payload decoder and its field rules, the
// token id, the signing digest, the signature check, the activation height
// and the mempool admission checker. CELLS20_state.go holds the token state.
//
// Switched off by default: with no activation height (0) every token
// transaction is refused at admission and the token state stays empty, so
// no block's state root changes. Below the activation height (always while
// none is set) a block is judged exactly as by a node without this code: a
// CELLS/token/v1 transaction that reaches a block anyway, for example from a
// producer that predates CELLS-20, is a plain CELL transfer
// (EnrollmentAwareApplier.ApplyTx). Adding this code therefore makes no
// block invalid that is valid today.
//
// Token amounts are integers (math/big) bounded to 2^256 - 1. The only
// float in this file is the transaction's CELL fee, which every transaction
// carries as float64 CELL; it must be exactly a whole number of dust
// (CELLS20FeeDust), and the signature covers that integer.

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"
	"sync/atomic"
	"unicode"
	"unicode/utf8"

	qcrypto "github.com/blackbeardONE/CELLS/pkg/crypto"
	"github.com/blackbeardONE/CELLS/pkg/mempool"
)

const (
	// CELLS20ContractID identifies CELLS-20 token transactions.
	CELLS20ContractID = "CELLS/token/v1"

	CELLS20ActionCreate   = "create"
	CELLS20ActionTransfer = "transfer"
	CELLS20ActionMint     = "mint"
	CELLS20ActionBurn     = "burn"
	CELLS20ActionRenounce = "renounce"

	// Minting modes of a token.
	CELLS20MintNone   = "none"
	CELLS20MintIssuer = "issuer"
	CELLS20MintOpen   = "open"

	// CELLS20TxIDPrefix starts every CELLS-20 transaction id. The rest of the
	// id is the lowercase hex signing digest, so the id is fixed by what the
	// sender signed and cannot be changed by whoever relays the transaction.
	CELLS20TxIDPrefix = "CELLS20-"

	// Field rules (CELLS_20.md, "Field rules").
	CELLS20MaxSymbolLen = 12
	CELLS20MaxNameLen   = 48
	CELLS20MaxDecimals  = 18

	// Domains of the three length-prefixed encodings.
	CELLS20IDDomain    = "CELLS/token/v1/id"
	CELLS20TxDomain    = "CELLS/token/v1/tx"
	CELLS20StateDomain = "CELLS/token/v1/state"
)

// CELLS20MaxUnits is 2^256 - 1, the largest amount any field may carry.
var CELLS20MaxUnits = new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))

// CELLS20MaxUnitDigits is the number of decimal digits of 2^256 - 1. A longer
// string is over the bound whatever its digits are, so it is refused before
// it is parsed.
var CELLS20MaxUnitDigits = len(CELLS20MaxUnits.String())

var (
	ErrNotCELLS20Tx = errors.New("chain: tx is not a CELLS-20 token transaction")
	// ErrCELLS20Inactive refuses every token transaction below the activation
	// height, and every one while no height is set.
	ErrCELLS20Inactive = errors.New("chain: CELLS-20 token transactions are not active")
	// ErrCELLS20Field refuses a malformed transaction: a bad field, a payload
	// that is not canonical JSON, or outer transaction fields that do not
	// fit a token transaction.
	ErrCELLS20Field = errors.New("chain: invalid CELLS-20 transaction")
	// ErrCELLS20Signature refuses a signature that does not verify, or a
	// public key that does not hash to the sender.
	ErrCELLS20Signature         = errors.New("chain: invalid CELLS-20 signature")
	ErrCELLS20UnknownToken      = errors.New("chain: unknown CELLS-20 token")
	ErrCELLS20TokenExists       = errors.New("chain: CELLS-20 token already exists")
	ErrCELLS20NotIssuer         = errors.New("chain: only the token's issuer may do this")
	ErrCELLS20MintingOff        = errors.New("chain: minting is off for this token")
	ErrCELLS20OpenMintRecipient = errors.New("chain: an open mint must go to its sender")
	ErrCELLS20OverMintLimit     = errors.New("chain: amount is over the token's mint_limit")
	ErrCELLS20OverMaxSupply     = errors.New("chain: the mint would pass the token's max_supply")
	ErrCELLS20InsufficientUnits = errors.New("chain: not enough token units")
)

var CELLS20ActivationHeight atomic.Uint64

// SetCELLS20ActivationHeight sets the first block height at which CELLS-20
// token transactions are valid. Zero (the default) keeps them off. Every
// validator must use the same value.
func SetCELLS20ActivationHeight(h uint64) { CELLS20ActivationHeight.Store(h) }

// CELLS20ActivationHeight reports the configured height (0 = off).
func CELLS20ActivationHeight() uint64 { return CELLS20ActivationHeight.Load() }

// CELLS20ActiveAt reports whether token transactions are valid in a block at
// height.
func CELLS20ActiveAt(height uint64) bool {
	h := CELLS20ActivationHeight.Load()
	return h != 0 && height >= h
}

// checkCELLS20Active is the activation rule for a block at height.
func checkCELLS20Active(height uint64) error {
	if CELLS20ActiveAt(height) {
		return nil
	}
	h := CELLS20ActivationHeight()
	if h == 0 {
		return fmt.Errorf("%w: no activation height is set", ErrCELLS20Inactive)
	}
	return fmt.Errorf("%w: block %d is below the activation height %d", ErrCELLS20Inactive, height, h)
}

// CELLS20HeightFrom resolves the height a token transaction is judged at
// from a block-height source (the next block's height). With no activation
// height it refuses without asking the source; with one, a missing source
// fails closed rather than judging at height 0.
func CELLS20HeightFrom(next func() (uint64, bool)) (uint64, error) {
	if CELLS20ActivationHeight() == 0 {
		return 0, checkCELLS20Active(0)
	}
	if next == nil {
		return 0, ErrEnrollmentHeightUnset
	}
	h, ok := next()
	if !ok {
		return 0, ErrEnrollmentHeightUnset
	}
	if err := checkCELLS20Active(h); err != nil {
		return 0, err
	}
	return h, nil
}

// CELLS20RulesAt reports whether the token rules judge a CELLS/token/v1
// transaction in the next block (next gives its height), and that height.
// With no activation height they never do, and next is not asked. With one,
// a missing source fails closed rather than judging at height 0.
func CELLS20RulesAt(next func() (uint64, bool)) (active bool, height uint64, err error) {
	if CELLS20ActivationHeight() == 0 {
		return false, 0, nil
	}
	if next == nil {
		return false, 0, ErrEnrollmentHeightUnset
	}
	h, ok := next()
	if !ok {
		return false, 0, ErrEnrollmentHeightUnset
	}
	return CELLS20ActiveAt(h), h, nil
}

// CELLS20Action is the payload of a CELLS-20 transaction. Each action carries
// exactly its own fields (CELLS_20.md, "Transactions"); every other field is
// absent. The field order is the canonical JSON key order.
type CELLS20Action struct {
	Action string `json:"action"`

	// create
	Symbol        string `json:"symbol,omitempty"`
	Name          string `json:"name,omitempty"`
	Decimals      *uint8 `json:"decimals,omitempty"`
	MaxSupply     string `json:"max_supply,omitempty"`
	InitialSupply string `json:"initial_supply,omitempty"`
	Mint          string `json:"mint,omitempty"`
	MintLimit     string `json:"mint_limit,omitempty"`

	// transfer, mint, burn, renounce
	Token  string `json:"token,omitempty"`
	To     string `json:"to,omitempty"`
	Amount string `json:"amount,omitempty"`
}

// CELLS20Decimals returns a pointer to d, for building a create action.
func CELLS20Decimals(d uint8) *uint8 { return &d }

func CELLS20FieldError(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrCELLS20Field, fmt.Sprintf(format, args...))
}

// DecodeCELLS20Payload decodes a payload and applies the field rules. The
// payload must be exactly the compact JSON encoding/json writes for the
// decoded action, so one action has one payload: no other key order, no
// spaces, no unknown, duplicate or empty fields.
func DecodeCELLS20Payload(payload []byte) (CELLS20Action, error) {
	var a CELLS20Action
	if err := json.Unmarshal(payload, &a); err != nil {
		return CELLS20Action{}, CELLS20FieldError("payload is not a JSON action: %v", err)
	}
	canonical, err := json.Marshal(a)
	if err != nil {
		return CELLS20Action{}, CELLS20FieldError("canonicalize payload: %v", err)
	}
	if !bytes.Equal(canonical, payload) {
		return CELLS20Action{}, CELLS20FieldError("payload is not canonical JSON")
	}
	if err := ValidateCELLS20Action(a); err != nil {
		return CELLS20Action{}, err
	}
	return a, nil
}

// ValidateCELLS20Action applies the field rules of CELLS_20.md to one action.
// It needs no chain state.
func ValidateCELLS20Action(a CELLS20Action) error {
	switch a.Action {
	case CELLS20ActionCreate:
		if a.Token != "" || a.To != "" || a.Amount != "" {
			return CELLS20FieldError("create carries only symbol, name, decimals, max_supply, initial_supply, mint and mint_limit")
		}
		if !validCELLS20Symbol(a.Symbol) {
			return CELLS20FieldError("symbol must be 1 to %d characters A to Z and 0 to 9", CELLS20MaxSymbolLen)
		}
		if !validCELLS20Name(a.Name) {
			return CELLS20FieldError("name must be 1 to %d characters of valid UTF-8 with no control characters", CELLS20MaxNameLen)
		}
		if a.Decimals == nil || *a.Decimals > CELLS20MaxDecimals {
			return CELLS20FieldError("decimals must be 0 to %d", CELLS20MaxDecimals)
		}
		maxSupply, err := parseCELLS20Units("max_supply", a.MaxSupply, false)
		if err != nil {
			return err
		}
		initial, err := parseCELLS20Units("initial_supply", a.InitialSupply, true)
		if err != nil {
			return err
		}
		if initial.Cmp(maxSupply) > 0 {
			return CELLS20FieldError("initial_supply is over max_supply")
		}
		switch a.Mint {
		case CELLS20MintNone:
			if a.MintLimit != "" {
				return CELLS20FieldError("mint_limit is present only in open mode")
			}
			if initial.Cmp(maxSupply) != 0 {
				return CELLS20FieldError("in mint none, initial_supply must equal max_supply")
			}
		case CELLS20MintIssuer:
			if a.MintLimit != "" {
				return CELLS20FieldError("mint_limit is present only in open mode")
			}
		case CELLS20MintOpen:
			if _, err := parseCELLS20Units("mint_limit", a.MintLimit, false); err != nil {
				return err
			}
		default:
			return CELLS20FieldError("mint must be none, issuer or open")
		}
		return nil
	case CELLS20ActionTransfer, CELLS20ActionMint:
		if hasCELLS20CreateFields(a) {
			return CELLS20FieldError("%s carries only token, to and amount", a.Action)
		}
		if !validCELLS20Hex64(a.Token) {
			return CELLS20FieldError("token must be a token id (64 lowercase hex characters)")
		}
		if !validCELLS20Hex64(a.To) {
			return CELLS20FieldError("to must be a CELLS address (64 lowercase hex characters)")
		}
		_, err := parseCELLS20Units("amount", a.Amount, false)
		return err
	case CELLS20ActionBurn:
		if hasCELLS20CreateFields(a) || a.To != "" {
			return CELLS20FieldError("burn carries only token and amount")
		}
		if !validCELLS20Hex64(a.Token) {
			return CELLS20FieldError("token must be a token id (64 lowercase hex characters)")
		}
		_, err := parseCELLS20Units("amount", a.Amount, false)
		return err
	case CELLS20ActionRenounce:
		if hasCELLS20CreateFields(a) || a.To != "" || a.Amount != "" {
			return CELLS20FieldError("renounce carries only token")
		}
		if !validCELLS20Hex64(a.Token) {
			return CELLS20FieldError("token must be a token id (64 lowercase hex characters)")
		}
		return nil
	default:
		return CELLS20FieldError("action must be create, transfer, mint, burn or renounce")
	}
}

func hasCELLS20CreateFields(a CELLS20Action) bool {
	return a.Symbol != "" || a.Name != "" || a.Decimals != nil || a.MaxSupply != "" ||
		a.InitialSupply != "" || a.Mint != "" || a.MintLimit != ""
}

// validCELLS20Symbol: 1 to 12 characters A-Z and 0-9.
func validCELLS20Symbol(s string) bool {
	if s == "" || len(s) > CELLS20MaxSymbolLen {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= 'A' && c <= 'Z' || c >= '0' && c <= '9') {
			return false
		}
	}
	return true
}

// validCELLS20Name: 1 to 48 Unicode characters (code points) of valid UTF-8,
// none of them a control character (category Cc).
func validCELLS20Name(s string) bool {
	if s == "" || !utf8.ValidString(s) {
		return false
	}
	n := 0
	for _, r := range s {
		if unicode.IsControl(r) {
			return false
		}
		n++
	}
	return n <= CELLS20MaxNameLen
}

// validCELLS20Hex64 accepts exactly 64 lowercase hex characters: a token id
// or a CELLS address.
func validCELLS20Hex64(s string) bool {
	if len(s) != 2*sha256.Size {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

// parseCELLS20Units parses a base-unit amount: decimal digits with no sign and
// no leading zeros, from 1 (or 0 when allowZero) to 2^256 - 1.
func parseCELLS20Units(field, s string, allowZero bool) (*big.Int, error) {
	if s == "" {
		return nil, CELLS20FieldError("%s is required", field)
	}
	if len(s) > CELLS20MaxUnitDigits {
		return nil, CELLS20FieldError("%s is over 2^256 - 1", field)
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return nil, CELLS20FieldError("%s must be a decimal string of base units", field)
		}
	}
	if len(s) > 1 && s[0] == '0' {
		return nil, CELLS20FieldError("%s has a leading zero", field)
	}
	v, ok := new(big.Int).SetString(s, 10)
	if !ok {
		return nil, CELLS20FieldError("%s must be a decimal string of base units", field)
	}
	if v.Sign() == 0 && !allowZero {
		return nil, CELLS20FieldError("%s must be at least 1", field)
	}
	if v.Cmp(CELLS20MaxUnits) > 0 {
		return nil, CELLS20FieldError("%s is over 2^256 - 1", field)
	}
	return v, nil
}

// CELLS20TokenID derives the id of the token that creator creates with the
// transaction at nonce: the lowercase hex SHA-256 of the length-prefixed
// fields domain CELLS/token/v1/id, creator and nonce.
func CELLS20TokenID(creator string, nonce uint64) string {
	var b strings.Builder
	writeTagged(&b, "domain", CELLS20IDDomain)
	writeTagged(&b, "creator", creator)
	writeTagged(&b, "nonce", strconv.FormatUint(nonce, 10))
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}

// CELLS20SigningPreimage is the length-prefixed encoding the signature covers
// (CELLS_20.md, "Signature"): the domain, the sender, the nonce, the fee in
// dust, the action and then the action's fields in the order of the table.
// A create's absent mint_limit is written as an empty value.
func CELLS20SigningPreimage(sender string, nonce, feeDust uint64, a CELLS20Action) string {
	var b strings.Builder
	writeTagged(&b, "domain", CELLS20TxDomain)
	writeTagged(&b, "sender", sender)
	writeTagged(&b, "nonce", strconv.FormatUint(nonce, 10))
	writeTagged(&b, "fee_dust", strconv.FormatUint(feeDust, 10))
	writeTagged(&b, "action", a.Action)
	switch a.Action {
	case CELLS20ActionCreate:
		decimals := ""
		if a.Decimals != nil {
			decimals = strconv.FormatUint(uint64(*a.Decimals), 10)
		}
		writeTagged(&b, "symbol", a.Symbol)
		writeTagged(&b, "name", a.Name)
		writeTagged(&b, "decimals", decimals)
		writeTagged(&b, "max_supply", a.MaxSupply)
		writeTagged(&b, "initial_supply", a.InitialSupply)
		writeTagged(&b, "mint", a.Mint)
		writeTagged(&b, "mint_limit", a.MintLimit)
	case CELLS20ActionTransfer, CELLS20ActionMint:
		writeTagged(&b, "token", a.Token)
		writeTagged(&b, "to", a.To)
		writeTagged(&b, "amount", a.Amount)
	case CELLS20ActionBurn:
		writeTagged(&b, "token", a.Token)
		writeTagged(&b, "amount", a.Amount)
	case CELLS20ActionRenounce:
		writeTagged(&b, "token", a.Token)
	}
	return b.String()
}

// CELLS20SigningDigest is the 32-byte message a CELLS-20 transaction's
// ML-DSA-87 signature covers.
func CELLS20SigningDigest(sender string, nonce, feeDust uint64, a CELLS20Action) []byte {
	sum := sha256.Sum256([]byte(CELLS20SigningPreimage(sender, nonce, feeDust, a)))
	return sum[:]
}

// CELLS20TxID is the transaction id for a signing digest.
func CELLS20TxID(digest []byte) string {
	return CELLS20TxIDPrefix + hex.EncodeToString(digest)
}

// CELLS20FeeCell is the tx.Fee value (CELL) of a fee of feeDust dust. It
// fails when the float does not convert back to exactly feeDust, which
// happens only for fees far above the CELL supply.
func CELLS20FeeCell(feeDust uint64) (float64, error) {
	fee := DustToCell(feeDust)
	if back, err := CELLS20FeeDust(fee); err != nil || back != feeDust {
		return 0, CELLS20FieldError("a fee of %d dust has no exact CELL value", feeDust)
	}
	return fee, nil
}

// CELLS20FeeDust returns the transaction fee in dust. The fee must be exactly
// DustToCell of a whole number of dust (bit for bit, so -0 is refused): one
// fee has one encoding, and the signature covers the integer.
func CELLS20FeeDust(fee float64) (uint64, error) {
	if math.IsNaN(fee) || math.IsInf(fee, 0) || fee < 0 {
		return 0, CELLS20FieldError("fee must be a finite amount of CELL, not below 0")
	}
	d, err := CellToDust(fee)
	if err != nil {
		return 0, CELLS20FieldError("fee is not a whole number of dust: %v", err)
	}
	if math.Float64bits(DustToCell(d)) != math.Float64bits(fee) {
		return 0, CELLS20FieldError("fee %v is not the exact CELL value of %d dust", fee, d)
	}
	return d, nil
}

// decodeCELLS20Tx checks everything but the signature and returns the action
// and the signing digest.
func decodeCELLS20Tx(tx *mempool.Tx) (CELLS20Action, []byte, error) {
	if tx == nil {
		return CELLS20Action{}, nil, errors.New("chain: nil CELLS-20 tx")
	}
	if tx.ContractID != CELLS20ContractID {
		return CELLS20Action{}, nil, fmt.Errorf("%w: got %q, want %q", ErrNotCELLS20Tx, tx.ContractID, CELLS20ContractID)
	}
	if math.Float64bits(tx.Amount) != 0 || tx.Recipient != "" || tx.GasLimit != 0 {
		return CELLS20Action{}, nil, CELLS20FieldError("a token transaction has CELL amount 0, an empty recipient and gas limit 0")
	}
	if !validCELLS20Hex64(tx.Sender) {
		return CELLS20Action{}, nil, CELLS20FieldError("sender must be a CELLS address (64 lowercase hex characters)")
	}
	feeDust, err := CELLS20FeeDust(tx.Fee)
	if err != nil {
		return CELLS20Action{}, nil, err
	}
	a, err := DecodeCELLS20Payload(tx.Payload)
	if err != nil {
		return CELLS20Action{}, nil, err
	}
	digest := CELLS20SigningDigest(tx.Sender, tx.Nonce, feeDust, a)
	if want := CELLS20TxID(digest); tx.ID != want {
		return CELLS20Action{}, nil, CELLS20FieldError("tx id must be %s", want)
	}
	return a, digest, nil
}

// DecodeCELLS20Tx checks a token transaction's shape, payload, fee and id
// without its signature.
func DecodeCELLS20Tx(tx *mempool.Tx) (CELLS20Action, error) {
	a, _, err := decodeCELLS20Tx(tx)
	return a, err
}

// VerifyCELLS20Tx performs every stateless check of a token transaction:
// DecodeCELLS20Tx and the ML-DSA-87 signature by the sender's key. It is run
// at admission and again whenever a transaction is applied.
func VerifyCELLS20Tx(tx *mempool.Tx) (CELLS20Action, error) {
	a, digest, err := decodeCELLS20Tx(tx)
	if err != nil {
		return CELLS20Action{}, err
	}
	if err := verifyCELLS20Signature(tx.Sender, tx.PublicKey, tx.Signature, digest); err != nil {
		return CELLS20Action{}, err
	}
	return a, nil
}

// verifyCELLS20Signature is the self-certifying ML-DSA-87 check every signed
// CELLS transaction uses (recovery capsules, streams, signed transfers): the
// public key must hash (SHA-256) to the sender's address and the signature
// must verify under it. The key and signature are lowercase hex, so a signed
// transaction has one encoding.
func verifyCELLS20Signature(sender, publicKeyHex, signatureHex string, digest []byte) error {
	if !validCELLS20LowerHex(publicKeyHex) || !validCELLS20LowerHex(signatureHex) {
		return fmt.Errorf("%w: public key and signature must be lowercase hex", ErrCELLS20Signature)
	}
	publicKey, err := hex.DecodeString(publicKeyHex)
	if err != nil {
		return fmt.Errorf("%w: public key is not valid hex", ErrCELLS20Signature)
	}
	signature, err := hex.DecodeString(signatureHex)
	if err != nil {
		return fmt.Errorf("%w: signature is not valid hex", ErrCELLS20Signature)
	}
	if len(publicKey) != mldsa87PublicKeyLen || len(signature) != mldsa87SignatureLen {
		return fmt.Errorf("%w: expected ML-DSA-87 key/signature sizes", ErrCELLS20Signature)
	}
	if BFTValidatorAddress(publicKey) != sender {
		return fmt.Errorf("%w: public key does not hash to the sender", ErrCELLS20Signature)
	}
	verifier := qcrypto.NewDilithiumVerifyOnly()
	if verifier == nil {
		return fmt.Errorf("%w: ML-DSA verifier unavailable", ErrCELLS20Signature)
	}
	defer verifier.Free()
	ok, err := verifier.VerifyWithPublicKey(digest, signature, publicKey)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrCELLS20Signature, err)
	}
	if !ok {
		return ErrCELLS20Signature
	}
	return nil
}

func validCELLS20LowerHex(s string) bool {
	if s == "" || len(s)%2 != 0 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

// CELLS20AdmissionChecker refuses token transactions that cannot be valid in
// the next block before they take mempool space: below the activation height
// (judged at nextHeight, the next block's height; always while no height is
// set) and on any stateless check. Stateful rules (units, issuer, supply)
// are checked when the transaction is applied. Other contracts go to next.
func CELLS20AdmissionChecker(nextHeight func() (uint64, bool), next func(*mempool.Tx) error) func(*mempool.Tx) error {
	return func(tx *mempool.Tx) error {
		if tx == nil {
			return errors.New("CELLS-20 admit: nil tx")
		}
		if tx.ContractID != CELLS20ContractID {
			if next != nil {
				return next(tx)
			}
			return nil
		}
		if _, err := CELLS20HeightFrom(nextHeight); err != nil {
			return fmt.Errorf("CELLS-20 admit: %w", err)
		}
		if _, err := VerifyCELLS20Tx(tx); err != nil {
			return fmt.Errorf("CELLS-20 admit: %w", err)
		}
		return nil
	}
}
