package CELLS

// CELLS-20 native tokens (CELLS/docs/docs/CELLS_20.md).
//
// A token action (create, transfer, mint, burn or renounce) is a transaction
// the sender signs with the same ML-DSA-87 key that holds their CELL. The
// signature covers the SHA-256 of a length-prefixed encoding of the sender,
// the nonce, the fee in dust and the action's fields (CELLS20SigningDigest),
// not JSON, so there is one thing to sign and the node checks exactly that.
// As with CELL transfers the SDK never holds a private key:
//
//  1. PrepareCELLS20 reads the sender's account nonce
//     (GET /api/v1/mining/account) and returns an unsigned CELLS20Transaction.
//  2. SignCELLS20 signs its SigningDigest with a TransferSigner.
//  3. SubmitCELLS20 posts it to POST /api/v1/CELLS20/submit.
//
// SendCELLS20 runs all three. Nodes refuse token transactions until the
// network sets an activation height: GET /api/v1/status reports it as
// consensus_auth.CELLS20_activation_height, and consensus_auth.CELLS20_active
// says whether the next block takes them.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// CELLS-20 identifiers. They mirror pkg/chain in the node.
const (
	// CELLS20ContractID is the contract_id of every token transaction.
	CELLS20ContractID = "CELLS/token/v1"
	// CELLS20TxIDPrefix starts every token transaction id; the rest is the
	// lowercase hex signing digest.
	CELLS20TxIDPrefix = "CELLS20-"

	CELLS20ActionCreate   = "create"
	CELLS20ActionTransfer = "transfer"
	CELLS20ActionMint     = "mint"
	CELLS20ActionBurn     = "burn"
	CELLS20ActionRenounce = "renounce"

	// Minting modes of a token.
	CELLS20MintNone   = "none"
	CELLS20MintIssuer = "issuer"
	CELLS20MintOpen   = "open"

	// Field rules (CELLS_20.md, "Field rules").
	CELLS20MaxSymbolLength = 12
	CELLS20MaxNameLength   = 48
	CELLS20MaxDecimals     = 18

	// CELLS20MaxUnits is 2^256 - 1, the largest amount any field may carry,
	// as the decimal string the fields use.
	CELLS20MaxUnits = "115792089237316195423570985008687907853269984665640564039457584007913129639935"

	CELLS20IDDomain = "CELLS/token/v1/id"
	CELLS20TxDomain = "CELLS/token/v1/tx"
)

// ErrCELLS20Field is wrapped by every error ValidateCELLS20Action returns.
var ErrCELLS20Field = errors.New("CELLS: invalid CELLS-20 action")

var CELLS20MaxUnits, _ = new(big.Int).SetString(CELLS20MaxUnits, 10)

// CELLS20Action is the payload of a token transaction: the action and its
// fields. Each action carries exactly the fields of its row in CELLS_20.md;
// leave the others empty. Amounts are whole base units as decimal strings
// (a token with 8 decimals counts 1.5 tokens as "150000000").
//
// The field order and JSON tags mirror pkg/chain.CELLS20Action in the node.
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

// CELLS20Decimals returns a pointer to d, for CELLS20Action.Decimals.
func CELLS20Decimals(d uint8) *uint8 { return &d }

func CELLS20FieldError(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrCELLS20Field, fmt.Sprintf(format, args...))
}

// ValidateCELLS20Action applies the field rules of CELLS_20.md, the same rules
// a node applies before it looks at chain state. A node can still refuse an
// action that passes, for a reason that needs the chain: an unknown token,
// a sender who is not the issuer, too few units, or a mint past max_supply.
func ValidateCELLS20Action(a CELLS20Action) error {
	switch a.Action {
	case CELLS20ActionCreate:
		if a.Token != "" || a.To != "" || a.Amount != "" {
			return CELLS20FieldError("create carries only symbol, name, decimals, max_supply, initial_supply, mint and mint_limit")
		}
		if !validCELLS20Symbol(a.Symbol) {
			return CELLS20FieldError("symbol must be 1 to %d characters A to Z and 0 to 9", CELLS20MaxSymbolLength)
		}
		if !validCELLS20Name(a.Name) {
			return CELLS20FieldError("name must be 1 to %d characters of valid UTF-8 with no control characters", CELLS20MaxNameLength)
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

func validCELLS20Symbol(s string) bool {
	if s == "" || len(s) > CELLS20MaxSymbolLength {
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
	return n <= CELLS20MaxNameLength
}

func validCELLS20Hex64(s string) bool {
	if len(s) != 64 {
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

func parseCELLS20Units(field, s string, allowZero bool) (*big.Int, error) {
	if s == "" {
		return nil, CELLS20FieldError("%s is required", field)
	}
	if len(s) > len(CELLS20MaxUnits) {
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

// writeCELLS20Field appends one length-prefixed field:
// "|" + tag + ":" + byte length of value + ":" + value.
func writeCELLS20Field(b *strings.Builder, tag, value string) {
	b.WriteString("|")
	b.WriteString(tag)
	b.WriteString(":")
	b.WriteString(strconv.Itoa(len(value)))
	b.WriteString(":")
	b.WriteString(value)
}

// CELLS20TokenID returns the id of the token that creator creates with the
// transaction at nonce. It is known before the transaction is sent.
func CELLS20TokenID(creator string, nonce uint64) string {
	var b strings.Builder
	writeCELLS20Field(&b, "domain", CELLS20IDDomain)
	writeCELLS20Field(&b, "creator", creator)
	writeCELLS20Field(&b, "nonce", strconv.FormatUint(nonce, 10))
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}

// CELLS20SigningPreimage returns the length-prefixed encoding whose SHA-256
// the signature covers (CELLS_20.md, "Signature"). It is exported for
// debugging and for other implementations; sign CELLS20SigningDigest.
func CELLS20SigningPreimage(sender string, nonce, feeDust uint64, a CELLS20Action) string {
	var b strings.Builder
	writeCELLS20Field(&b, "domain", CELLS20TxDomain)
	writeCELLS20Field(&b, "sender", sender)
	writeCELLS20Field(&b, "nonce", strconv.FormatUint(nonce, 10))
	writeCELLS20Field(&b, "fee_dust", strconv.FormatUint(feeDust, 10))
	writeCELLS20Field(&b, "action", a.Action)
	switch a.Action {
	case CELLS20ActionCreate:
		decimals := ""
		if a.Decimals != nil {
			decimals = strconv.FormatUint(uint64(*a.Decimals), 10)
		}
		writeCELLS20Field(&b, "symbol", a.Symbol)
		writeCELLS20Field(&b, "name", a.Name)
		writeCELLS20Field(&b, "decimals", decimals)
		writeCELLS20Field(&b, "max_supply", a.MaxSupply)
		writeCELLS20Field(&b, "initial_supply", a.InitialSupply)
		writeCELLS20Field(&b, "mint", a.Mint)
		writeCELLS20Field(&b, "mint_limit", a.MintLimit)
	case CELLS20ActionTransfer, CELLS20ActionMint:
		writeCELLS20Field(&b, "token", a.Token)
		writeCELLS20Field(&b, "to", a.To)
		writeCELLS20Field(&b, "amount", a.Amount)
	case CELLS20ActionBurn:
		writeCELLS20Field(&b, "token", a.Token)
		writeCELLS20Field(&b, "amount", a.Amount)
	case CELLS20ActionRenounce:
		writeCELLS20Field(&b, "token", a.Token)
	}
	return b.String()
}

// CELLS20SigningDigest returns the 32 bytes a token transaction's ML-DSA-87
// signature covers (FIPS 204, empty context).
func CELLS20SigningDigest(sender string, nonce, feeDust uint64, a CELLS20Action) []byte {
	sum := sha256.Sum256([]byte(CELLS20SigningPreimage(sender, nonce, feeDust, a)))
	return sum[:]
}

// CELLS20TxID returns the transaction id for a signing digest.
func CELLS20TxID(digest []byte) string {
	return CELLS20TxIDPrefix + hex.EncodeToString(digest)
}

// CELLS20Transaction is a token transaction as POST /api/v1/CELLS20/submit
// takes it. The node builds the chain transaction from these fields (the
// canonical JSON payload, the fee in CELL and the id) and checks it against
// its current state before its mempool takes it.
type CELLS20Transaction struct {
	// Sender must equal WalletAddress(public key).
	Sender string `json:"sender"`
	// Nonce is the sender's current account nonce: the nonce of
	// GET /api/v1/mining/account (PrepareCELLS20 reads it).
	Nonce uint64 `json:"nonce"`
	// FeeDust is the fee in dust (1 CELL = 100,000,000 dust), paid from the
	// sender's CELL and burned. It may be 0.
	FeeDust uint64       `json:"fee_dust"`
	Payload CELLS20Action `json:"payload"`
	// PublicKey and Signature are lowercase hex (ML-DSA-87).
	PublicKey string `json:"public_key"`
	Signature string `json:"signature"`
}

// SigningDigest returns the 32 bytes to sign.
func (t CELLS20Transaction) SigningDigest() []byte {
	return CELLS20SigningDigest(t.Sender, t.Nonce, t.FeeDust, t.Payload)
}

// TxID returns the id the node gives the transaction, for GetReceipt.
func (t CELLS20Transaction) TxID() string { return CELLS20TxID(t.SigningDigest()) }

// TokenID returns the token the transaction acts on; for a create, the id
// the new token gets.
func (t CELLS20Transaction) TokenID() string {
	if t.Payload.Action == CELLS20ActionCreate {
		return CELLS20TokenID(t.Sender, t.Nonce)
	}
	return t.Payload.Token
}

// IsSigned reports whether the transaction carries a signature and a public key.
func (t CELLS20Transaction) IsSigned() bool { return t.Signature != "" && t.PublicKey != "" }

// SignCELLS20 signs tx with signer and returns the signed copy. tx.Sender
// must be the signer's wallet address and tx.Payload must pass
// ValidateCELLS20Action.
func SignCELLS20(ctx context.Context, tx CELLS20Transaction, signer TransferSigner) (CELLS20Transaction, error) {
	if signer == nil {
		return CELLS20Transaction{}, errors.New("CELLS: a TransferSigner is required")
	}
	if err := ValidateCELLS20Action(tx.Payload); err != nil {
		return CELLS20Transaction{}, err
	}
	publicKey := signer.PublicKey()
	if len(publicKey) != MLDSA87PublicKeySize {
		return CELLS20Transaction{}, fmt.Errorf("CELLS: signer public key is %d bytes, want %d (ML-DSA-87)",
			len(publicKey), MLDSA87PublicKeySize)
	}
	if address := WalletAddress(publicKey); tx.Sender != address {
		return CELLS20Transaction{}, fmt.Errorf("CELLS: token transaction sender %q is not the signer's wallet address %s",
			tx.Sender, address)
	}
	signature, err := signer.Sign(ctx, tx.SigningDigest())
	if err != nil {
		return CELLS20Transaction{}, fmt.Errorf("CELLS: sign token transaction: %w", err)
	}
	if len(signature) != MLDSA87SignatureSize {
		return CELLS20Transaction{}, fmt.Errorf("CELLS: signer returned a %d-byte signature, want %d (ML-DSA-87)",
			len(signature), MLDSA87SignatureSize)
	}
	tx.PublicKey = hex.EncodeToString(publicKey)
	tx.Signature = hex.EncodeToString(signature)
	return tx, nil
}

// AccountInfo is the body of GET /api/v1/mining/account: the consensus CELL
// balance and nonce of an address.
type AccountInfo struct {
	Address string  `json:"address"`
	Balance float64 `json:"balance"`
	// Nonce is the account nonce a token transaction carries.
	Nonce uint64 `json:"nonce"`
	// Present is false for an address that never received CELL.
	Present bool `json:"present"`
}

// GetAccount returns the consensus CELL balance and nonce of an address
// (GET /api/v1/mining/account, public).
func (c *Client) GetAccount(ctx context.Context, address string) (*AccountInfo, error) {
	q := url.Values{}
	q.Set("address", address)
	var info AccountInfo
	if err := c.do(ctx, http.MethodGet, "/api/v1/mining/account?"+q.Encode(), nil, &info); err != nil {
		return nil, err
	}
	return &info, nil
}

// PrepareCELLS20 returns an unsigned token transaction from sender at the
// sender's current account nonce. The sender needs a CELL account (the fee,
// even 0, is charged to it), so an address that never received CELL is
// refused here rather than by the node.
func (c *Client) PrepareCELLS20(ctx context.Context, sender string, action CELLS20Action, feeDust uint64) (CELLS20Transaction, error) {
	if !walletAddressPattern.MatchString(sender) {
		return CELLS20Transaction{}, fmt.Errorf("CELLS: token transaction sender %q is not a 64-character lowercase hex wallet address", sender)
	}
	if err := ValidateCELLS20Action(action); err != nil {
		return CELLS20Transaction{}, err
	}
	account, err := c.GetAccount(ctx, sender)
	if err != nil {
		return CELLS20Transaction{}, err
	}
	if !account.Present {
		return CELLS20Transaction{}, fmt.Errorf("CELLS: %s has no CELL account yet; a token transaction's sender needs one", sender)
	}
	return CELLS20Transaction{Sender: sender, Nonce: account.Nonce, FeeDust: feeDust, Payload: action}, nil
}

// CELLS20SubmitResponse is the node's answer to an admitted token transaction.
type CELLS20SubmitResponse struct {
	TxID   string `json:"tx_id"`
	Action string `json:"action"`
	Sender string `json:"sender"`
	Nonce  uint64 `json:"nonce"`
	// Token is the token acted on; for a create, the id the new token gets.
	Token  string `json:"token"`
	Status string `json:"status"`
	// MempoolStatus is "submitted", or "duplicate" when the node already had it.
	MempoolStatus string `json:"mempool_status"`
}

// SubmitCELLS20 posts a signed token transaction
// (POST /api/v1/CELLS20/submit, public). A node refuses a transaction that
// breaks a rule with HTTP 422 (*ErrAPI), with nothing charged; that
// includes every token transaction before the activation height.
func (c *Client) SubmitCELLS20(ctx context.Context, tx CELLS20Transaction) (*CELLS20SubmitResponse, error) {
	if !tx.IsSigned() {
		return nil, errors.New("CELLS: the token transaction is not signed (SignCELLS20)")
	}
	var response CELLS20SubmitResponse
	if err := c.do(ctx, http.MethodPost, "/api/v1/CELLS20/submit", tx, &response); err != nil {
		return nil, err
	}
	if want := tx.TxID(); response.TxID != want {
		return &response, fmt.Errorf("CELLS: node answered tx_id %q for transaction %s", response.TxID, want)
	}
	return &response, nil
}

// SendCELLS20 prepares, signs and submits a token transaction from the
// signer's wallet.
func (c *Client) SendCELLS20(ctx context.Context, action CELLS20Action, feeDust uint64, signer TransferSigner) (*CELLS20SubmitResponse, error) {
	if signer == nil {
		return nil, errors.New("CELLS: a TransferSigner is required")
	}
	tx, err := c.PrepareCELLS20(ctx, WalletAddress(signer.PublicKey()), action, feeDust)
	if err != nil {
		return nil, err
	}
	signed, err := SignCELLS20(ctx, tx, signer)
	if err != nil {
		return nil, err
	}
	return c.SubmitCELLS20(ctx, signed)
}

// CELLS20Token is one token as the read routes return it. Amounts are base
// units as decimal strings; divide by 10^Decimals to show whole tokens.
type CELLS20Token struct {
	ID            string `json:"id"`
	Symbol        string `json:"symbol"`
	Name          string `json:"name"`
	Decimals      uint8  `json:"decimals"`
	Issuer        string `json:"issuer"`
	Mint          string `json:"mint"`
	MintLimit     string `json:"mint_limit,omitempty"`
	MaxSupply     string `json:"max_supply"`
	Minted        string `json:"minted"`
	Burned        string `json:"burned"`
	Supply        string `json:"supply"`
	Holders       int    `json:"holders"`
	CreatedHeight uint64 `json:"created_height"`
}

// CELLS20Holder is one holder of a token.
type CELLS20Holder struct {
	Address string `json:"address"`
	Balance string `json:"balance"`
}

// CELLS20Balance is one token an address holds.
type CELLS20Balance struct {
	Token    string `json:"token"`
	Symbol   string `json:"symbol"`
	Name     string `json:"name"`
	Decimals uint8  `json:"decimals"`
	Balance  string `json:"balance"`
}

// CELLS20TokensPage is the body of GET /api/v1/CELLS20/tokens.
type CELLS20TokensPage struct {
	StateRoot  string        `json:"state_root"`
	Tokens     []CELLS20Token `json:"tokens"`
	NextCursor string        `json:"next_cursor,omitempty"`
	HasMore    bool          `json:"has_more"`
}

// CELLS20TokenInfo is the body of GET /api/v1/CELLS20/tokens/{id}.
type CELLS20TokenInfo struct {
	StateRoot string      `json:"state_root"`
	Token     CELLS20Token `json:"token"`
}

// CELLS20HoldersPage is the body of GET /api/v1/CELLS20/tokens/{id}/holders.
type CELLS20HoldersPage struct {
	StateRoot  string         `json:"state_root"`
	Token      string         `json:"token"`
	Holders    []CELLS20Holder `json:"holders"`
	NextCursor string         `json:"next_cursor,omitempty"`
	HasMore    bool           `json:"has_more"`
}

// CELLS20Balances is the body of GET /api/v1/CELLS20/balances/{address}.
type CELLS20Balances struct {
	StateRoot string          `json:"state_root"`
	Address   string          `json:"address"`
	Balances  []CELLS20Balance `json:"balances"`
}

func CELLS20PageQuery(cursor string, limit int) string {
	q := url.Values{}
	if cursor != "" {
		q.Set("cursor", cursor)
	}
	if limit > 0 {
		q.Set("limit", strconv.Itoa(limit))
	}
	if len(q) == 0 {
		return ""
	}
	return "?" + q.Encode()
}

// GetCELLS20Tokens returns tokens, newest first (GET /api/v1/CELLS20/tokens,
// public). limit 0 returns every remaining token; with a limit, pass the
// page's NextCursor as cursor to read the next page.
func (c *Client) GetCELLS20Tokens(ctx context.Context, cursor string, limit int) (*CELLS20TokensPage, error) {
	var page CELLS20TokensPage
	if err := c.do(ctx, http.MethodGet, "/api/v1/CELLS20/tokens"+CELLS20PageQuery(cursor, limit), nil, &page); err != nil {
		return nil, err
	}
	return &page, nil
}

// GetCELLS20Token returns one token (GET /api/v1/CELLS20/tokens/{id}, public).
// An unknown id is an *ErrAPI with IsNotFound.
func (c *Client) GetCELLS20Token(ctx context.Context, id string) (*CELLS20TokenInfo, error) {
	var info CELLS20TokenInfo
	if err := c.do(ctx, http.MethodGet, "/api/v1/CELLS20/tokens/"+url.PathEscape(id), nil, &info); err != nil {
		return nil, err
	}
	return &info, nil
}

// GetCELLS20Holders returns a token's holders, largest balance first
// (GET /api/v1/CELLS20/tokens/{id}/holders, public), paged like
// GetCELLS20Tokens.
func (c *Client) GetCELLS20Holders(ctx context.Context, id, cursor string, limit int) (*CELLS20HoldersPage, error) {
	var page CELLS20HoldersPage
	path := "/api/v1/CELLS20/tokens/" + url.PathEscape(id) + "/holders" + CELLS20PageQuery(cursor, limit)
	if err := c.do(ctx, http.MethodGet, path, nil, &page); err != nil {
		return nil, err
	}
	return &page, nil
}

// GetCELLS20Balances returns every token an address holds
// (GET /api/v1/CELLS20/balances/{address}, public).
func (c *Client) GetCELLS20Balances(ctx context.Context, address string) (*CELLS20Balances, error) {
	var balances CELLS20Balances
	if err := c.do(ctx, http.MethodGet, "/api/v1/CELLS20/balances/"+url.PathEscape(address), nil, &balances); err != nil {
		return nil, err
	}
	return &balances, nil
}
