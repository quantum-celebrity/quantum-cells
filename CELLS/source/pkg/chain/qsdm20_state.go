package chain

// CELLS20_state.go: the CELLS-20 token state (CELLS/docs/docs/CELLS_20.md, "State
// and the state root").
//
// For each token the chain keeps its id, symbol, name, decimals, issuer, mint
// mode, mint_limit, max_supply, minted, burned and creation height, and for
// each holder a non-zero balance. Supply is minted minus burned, and the sum
// of a token's balances always equals its supply.
//
// Applying an action has two steps under the store lock: plan checks the
// action against the state and computes the change without making it; only
// then is the CELL fee charged and the nonce bumped, and only after that does
// the change land. A refused action therefore charges nothing, moves no nonce
// and leaves the token state untouched, without copying the state first.
//
// Every amount is a *big.Int that is never modified in place: a change
// stores a new value, so a value handed out is never changed underneath.

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/blackbeardONE/CELLS/pkg/mempool"
)

// CELLS20Token is the public view of one token.
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

type CELLS20Token struct {
	id            string
	symbol        string
	name          string
	decimals      uint8
	issuer        string
	mint          string
	mintLimit     *big.Int // set only while mint is open
	maxSupply     *big.Int
	minted        *big.Int
	burned        *big.Int
	createdHeight uint64
	balances      map[string]*big.Int // non-zero balances only
}

func (t *CELLS20Token) balanceOf(address string) *big.Int {
	if b, ok := t.balances[address]; ok {
		return b
	}
	return new(big.Int)
}

// setBalance stores v; a zero balance is removed, so balances lists only
// holders.
func (t *CELLS20Token) setBalance(address string, v *big.Int) {
	if v.Sign() == 0 {
		delete(t.balances, address)
		return
	}
	t.balances[address] = v
}

func (t *CELLS20Token) clone() *CELLS20Token {
	cp := *t
	cp.mintLimit = copyCELLS20Units(t.mintLimit)
	cp.maxSupply = copyCELLS20Units(t.maxSupply)
	cp.minted = copyCELLS20Units(t.minted)
	cp.burned = copyCELLS20Units(t.burned)
	cp.balances = make(map[string]*big.Int, len(t.balances))
	for addr, b := range t.balances {
		cp.balances[addr] = copyCELLS20Units(b)
	}
	return &cp
}

func (t *CELLS20Token) view() CELLS20Token {
	return CELLS20Token{
		ID:            t.id,
		Symbol:        t.symbol,
		Name:          t.name,
		Decimals:      t.decimals,
		Issuer:        t.issuer,
		Mint:          t.mint,
		MintLimit:     CELLS20UnitsString(t.mintLimit),
		MaxSupply:     t.maxSupply.String(),
		Minted:        t.minted.String(),
		Burned:        t.burned.String(),
		Supply:        new(big.Int).Sub(t.minted, t.burned).String(),
		Holders:       len(t.balances),
		CreatedHeight: t.createdHeight,
	}
}

func copyCELLS20Units(v *big.Int) *big.Int {
	if v == nil {
		return nil
	}
	return new(big.Int).Set(v)
}

// CELLS20UnitsString renders an amount; an absent one (a mint_limit outside
// open mode) is the empty string.
func CELLS20UnitsString(v *big.Int) string {
	if v == nil {
		return ""
	}
	return v.String()
}

// CELLS20StateStore is the consensus token state.
type CELLS20StateStore struct {
	mu     sync.RWMutex
	tokens map[string]*CELLS20Token
}

// NewCELLS20StateStore returns an empty token state.
func NewCELLS20StateStore() *CELLS20StateStore {
	return &CELLS20StateStore{tokens: map[string]*CELLS20Token{}}
}

// ApplyTx implements StateApplier. It has no block height, and no height
// below 1 is ever active, so it refuses every transaction: use
// ApplyEconomicTxAtHeight or ApplyHistoricalTx.
func (s *CELLS20StateStore) ApplyTx(tx *mempool.Tx) error {
	return s.ApplyHistoricalTx(tx, 0)
}

// ApplyHistoricalTx rebuilds the token state from a committed block without
// touching accounts (balances and nonces come from the persisted account
// snapshot). height is the committed block's height.
func (s *CELLS20StateStore) ApplyHistoricalTx(tx *mempool.Tx, height uint64) error {
	if s == nil {
		return errors.New("chain: nil CELLS-20 state store")
	}
	if err := checkCELLS20Active(height); err != nil {
		return err
	}
	action, err := VerifyCELLS20Tx(tx)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	commit, err := s.planLocked(action, tx.Sender, tx.Nonce, height)
	if err != nil {
		return err
	}
	commit()
	return nil
}

// ApplyEconomicTxAtHeight applies a token transaction in a block at height:
// the token rules, then the CELL fee (burned) and the nonce, then the token
// change. A refused transaction changes nothing.
func (s *CELLS20StateStore) ApplyEconomicTxAtHeight(tx *mempool.Tx, accounts *AccountStore, height uint64) error {
	if s == nil {
		return errors.New("chain: nil CELLS-20 state store")
	}
	if accounts == nil {
		return errors.New("chain: nil AccountStore for CELLS-20 transaction")
	}
	if err := checkCELLS20Active(height); err != nil {
		return err
	}
	action, err := VerifyCELLS20Tx(tx)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	commit, err := s.planLocked(action, tx.Sender, tx.Nonce, height)
	if err != nil {
		return err
	}
	if err := accounts.ChargeAndBumpNonce(tx.Sender, tx.Fee, tx.Nonce); err != nil {
		return err
	}
	commit()
	return nil
}

// CheckEconomicTx reports whether tx would apply on the current state: the
// stateless checks and signature, the token rules and, when accounts is not
// nil, the sender's CELL account (it must exist, carry tx's nonce and cover
// the fee). It changes nothing and does not check the activation height.
func (s *CELLS20StateStore) CheckEconomicTx(tx *mempool.Tx, accounts *AccountStore) error {
	if s == nil {
		return errors.New("chain: nil CELLS-20 state store")
	}
	action, err := VerifyCELLS20Tx(tx)
	if err != nil {
		return err
	}
	s.mu.RLock()
	_, err = s.planLocked(action, tx.Sender, tx.Nonce, 0)
	s.mu.RUnlock()
	if err != nil {
		return err
	}
	if accounts == nil {
		return nil
	}
	acc, ok := accounts.Get(tx.Sender)
	if !ok {
		return fmt.Errorf("sender %s not found", tx.Sender)
	}
	if acc.Nonce != tx.Nonce {
		return fmt.Errorf("nonce mismatch for %s: expected %d, got %d", tx.Sender, acc.Nonce, tx.Nonce)
	}
	if acc.Balance < tx.Fee {
		return fmt.Errorf("insufficient balance: have %.8f, need %.8f", acc.Balance, tx.Fee)
	}
	return nil
}

func (s *CELLS20StateStore) tokenLocked(id string) (*CELLS20Token, error) {
	tok, ok := s.tokens[id]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrCELLS20UnknownToken, id)
	}
	return tok, nil
}

// planLocked checks action, sent by sender at nonce in a block at height,
// against the current state and returns the change it makes without making
// it. The caller holds s.mu; the returned commit must run under the write
// lock, before anything else changes the store.
func (s *CELLS20StateStore) planLocked(a CELLS20Action, sender string, nonce, height uint64) (func(), error) {
	if err := ValidateCELLS20Action(a); err != nil {
		return nil, err
	}
	switch a.Action {
	case CELLS20ActionCreate:
		id := CELLS20TokenID(sender, nonce)
		if _, exists := s.tokens[id]; exists {
			return nil, fmt.Errorf("%w: %s", ErrCELLS20TokenExists, id)
		}
		maxSupply, err := parseCELLS20Units("max_supply", a.MaxSupply, false)
		if err != nil {
			return nil, err
		}
		initial, err := parseCELLS20Units("initial_supply", a.InitialSupply, true)
		if err != nil {
			return nil, err
		}
		var limit *big.Int
		if a.Mint == CELLS20MintOpen {
			if limit, err = parseCELLS20Units("mint_limit", a.MintLimit, false); err != nil {
				return nil, err
			}
		}
		tok := &CELLS20Token{
			id:            id,
			symbol:        a.Symbol,
			name:          a.Name,
			decimals:      *a.Decimals,
			issuer:        sender,
			mint:          a.Mint,
			mintLimit:     limit,
			maxSupply:     maxSupply,
			minted:        initial,
			burned:        new(big.Int),
			createdHeight: height,
			balances:      map[string]*big.Int{},
		}
		tok.setBalance(sender, new(big.Int).Set(initial))
		return func() { s.tokens[id] = tok }, nil

	case CELLS20ActionTransfer:
		tok, err := s.tokenLocked(a.Token)
		if err != nil {
			return nil, err
		}
		amount, err := parseCELLS20Units("amount", a.Amount, false)
		if err != nil {
			return nil, err
		}
		from := tok.balanceOf(sender)
		if from.Cmp(amount) < 0 {
			return nil, fmt.Errorf("%w: %s holds %s, needs %s", ErrCELLS20InsufficientUnits, sender, from, amount)
		}
		if a.To == sender {
			return func() {}, nil
		}
		newFrom := new(big.Int).Sub(from, amount)
		newTo := new(big.Int).Add(tok.balanceOf(a.To), amount)
		to := a.To
		return func() {
			tok.setBalance(sender, newFrom)
			tok.setBalance(to, newTo)
		}, nil

	case CELLS20ActionMint:
		tok, err := s.tokenLocked(a.Token)
		if err != nil {
			return nil, err
		}
		amount, err := parseCELLS20Units("amount", a.Amount, false)
		if err != nil {
			return nil, err
		}
		switch tok.mint {
		case CELLS20MintIssuer:
			if sender != tok.issuer {
				return nil, ErrCELLS20NotIssuer
			}
		case CELLS20MintOpen:
			if a.To != sender {
				return nil, ErrCELLS20OpenMintRecipient
			}
			if tok.mintLimit == nil || amount.Cmp(tok.mintLimit) > 0 {
				return nil, fmt.Errorf("%w: %s > %s", ErrCELLS20OverMintLimit, amount, CELLS20UnitsString(tok.mintLimit))
			}
		default:
			return nil, ErrCELLS20MintingOff
		}
		newMinted := new(big.Int).Add(tok.minted, amount)
		if newMinted.Cmp(tok.maxSupply) > 0 {
			return nil, fmt.Errorf("%w: minted %s + %s > %s", ErrCELLS20OverMaxSupply, tok.minted, amount, tok.maxSupply)
		}
		newTo := new(big.Int).Add(tok.balanceOf(a.To), amount)
		to := a.To
		return func() {
			tok.minted = newMinted
			tok.setBalance(to, newTo)
		}, nil

	case CELLS20ActionBurn:
		tok, err := s.tokenLocked(a.Token)
		if err != nil {
			return nil, err
		}
		amount, err := parseCELLS20Units("amount", a.Amount, false)
		if err != nil {
			return nil, err
		}
		from := tok.balanceOf(sender)
		if from.Cmp(amount) < 0 {
			return nil, fmt.Errorf("%w: %s holds %s, needs %s", ErrCELLS20InsufficientUnits, sender, from, amount)
		}
		newFrom := new(big.Int).Sub(from, amount)
		newBurned := new(big.Int).Add(tok.burned, amount)
		return func() {
			tok.setBalance(sender, newFrom)
			tok.burned = newBurned
		}, nil

	case CELLS20ActionRenounce:
		tok, err := s.tokenLocked(a.Token)
		if err != nil {
			return nil, err
		}
		if sender != tok.issuer {
			return nil, ErrCELLS20NotIssuer
		}
		if tok.mint != CELLS20MintIssuer && tok.mint != CELLS20MintOpen {
			return nil, fmt.Errorf("%w: it is already off", ErrCELLS20MintingOff)
		}
		newMax := new(big.Int).Set(tok.minted)
		return func() {
			tok.mint = CELLS20MintNone
			tok.mintLimit = nil
			tok.maxSupply = newMax
		}, nil
	}
	return nil, CELLS20FieldError("action must be create, transfer, mint, burn or renounce")
}

// Count returns the number of tokens. The CELLS-20 segment joins the block
// state root once it is above zero.
func (s *CELLS20StateStore) Count() int {
	if s == nil {
		return 0
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.tokens)
}

// Token returns one token.
func (s *CELLS20StateStore) Token(id string) (CELLS20Token, bool) {
	if s == nil {
		return CELLS20Token{}, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	tok, ok := s.tokens[id]
	if !ok {
		return CELLS20Token{}, false
	}
	return tok.view(), true
}

// Tokens returns every token, newest first: by creation height descending,
// then by id ascending.
func (s *CELLS20StateStore) Tokens() []CELLS20Token {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	out := make([]CELLS20Token, 0, len(s.tokens))
	for _, tok := range s.tokens {
		out = append(out, tok.view())
	}
	s.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedHeight != out[j].CreatedHeight {
			return out[i].CreatedHeight > out[j].CreatedHeight
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// Holders returns a token's holders, largest balance first, then by address
// ascending.
func (s *CELLS20StateStore) Holders(id string) ([]CELLS20Holder, bool) {
	if s == nil {
		return nil, false
	}
	s.mu.RLock()
	tok, ok := s.tokens[id]
	if !ok {
		s.mu.RUnlock()
		return nil, false
	}
	type holding struct {
		address string
		balance *big.Int
	}
	rows := make([]holding, 0, len(tok.balances))
	for addr, b := range tok.balances {
		rows = append(rows, holding{address: addr, balance: b})
	}
	s.mu.RUnlock()
	sort.Slice(rows, func(i, j int) bool {
		if c := rows[i].balance.Cmp(rows[j].balance); c != 0 {
			return c > 0
		}
		return rows[i].address < rows[j].address
	})
	out := make([]CELLS20Holder, len(rows))
	for i, row := range rows {
		out[i] = CELLS20Holder{Address: row.address, Balance: row.balance.String()}
	}
	return out, true
}

// BalancesOf returns every token address holds, by token id ascending.
func (s *CELLS20StateStore) BalancesOf(address string) []CELLS20Balance {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	out := make([]CELLS20Balance, 0)
	for _, tok := range s.tokens {
		b, ok := tok.balances[address]
		if !ok {
			continue
		}
		out = append(out, CELLS20Balance{
			Token: tok.id, Symbol: tok.symbol, Name: tok.name, Decimals: tok.decimals, Balance: b.String(),
		})
	}
	s.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool { return out[i].Token < out[j].Token })
	return out
}

// ChainReplayClone implements ChainReplayApplier with a deep copy.
func (s *CELLS20StateStore) ChainReplayClone() ChainReplayApplier {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	clone := NewCELLS20StateStore()
	for id, tok := range s.tokens {
		clone.tokens[id] = tok.clone()
	}
	return clone
}

// RestoreFromChainReplay implements ChainReplayApplier: the store becomes a
// deep copy of from.
func (s *CELLS20StateStore) RestoreFromChainReplay(from ChainReplayApplier) error {
	if s == nil {
		return errors.New("chain: nil CELLS-20 state store")
	}
	other, ok := from.(*CELLS20StateStore)
	if !ok || other == nil {
		return errors.New("chain: CELLS-20 restore expects *CELLS20StateStore")
	}
	other.mu.RLock()
	tokens := make(map[string]*CELLS20Token, len(other.tokens))
	for id, tok := range other.tokens {
		tokens[id] = tok.clone()
	}
	other.mu.RUnlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tokens = tokens
	return nil
}

// StateRoot is the token state root: the SHA-256 of the length-prefixed
// listing of every token by id ascending, each followed by its non-zero
// balances by address ascending (CELLS_20.md, "State and the state root").
func (s *CELLS20StateStore) StateRoot() string {
	if s == nil {
		return ""
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	ids := make([]string, 0, len(s.tokens))
	for id := range s.tokens {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	h := sha256.New()
	var b strings.Builder
	writeTagged(&b, "domain", CELLS20StateDomain)
	h.Write([]byte(b.String()))
	for _, id := range ids {
		tok := s.tokens[id]
		b.Reset()
		writeTagged(&b, "token", tok.id)
		writeTagged(&b, "symbol", tok.symbol)
		writeTagged(&b, "name", tok.name)
		writeTagged(&b, "decimals", strconv.FormatUint(uint64(tok.decimals), 10))
		writeTagged(&b, "issuer", tok.issuer)
		writeTagged(&b, "mint", tok.mint)
		writeTagged(&b, "mint_limit", CELLS20UnitsString(tok.mintLimit))
		writeTagged(&b, "max_supply", tok.maxSupply.String())
		writeTagged(&b, "minted", tok.minted.String())
		writeTagged(&b, "burned", tok.burned.String())
		writeTagged(&b, "created_height", strconv.FormatUint(tok.createdHeight, 10))
		h.Write([]byte(b.String()))
		holders := make([]string, 0, len(tok.balances))
		for addr, bal := range tok.balances {
			if bal.Sign() > 0 {
				holders = append(holders, addr)
			}
		}
		sort.Strings(holders)
		for _, addr := range holders {
			b.Reset()
			writeTagged(&b, "holder", addr)
			writeTagged(&b, "balance", tok.balances[addr].String())
			h.Write([]byte(b.String()))
		}
	}
	return hex.EncodeToString(h.Sum(nil))
}

var (
	_ StateApplier       = (*CELLS20StateStore)(nil)
	_ ChainReplayApplier = (*CELLS20StateStore)(nil)
)
