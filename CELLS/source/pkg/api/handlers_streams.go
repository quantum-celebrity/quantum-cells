package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/blackbeardONE/CELLS/pkg/chain"
	"github.com/blackbeardONE/CELLS/pkg/mempool"
)

type CELLSStreamActionSubmitterHolder struct {
	mu   sync.RWMutex
	pool MempoolSubmitter
}

var CELLSStreamActionMempoolHolder = &CELLSStreamActionSubmitterHolder{}

// SetStreamActionMempool installs the live validator mempool used by signed
// CELL stream actions.
func SetStreamActionMempool(pool MempoolSubmitter) {
	CELLSStreamActionMempoolHolder.mu.Lock()
	defer CELLSStreamActionMempoolHolder.mu.Unlock()
	CELLSStreamActionMempoolHolder.pool = pool
}

func currentStreamActionMempool() MempoolSubmitter {
	CELLSStreamActionMempoolHolder.mu.RLock()
	defer CELLSStreamActionMempoolHolder.mu.RUnlock()
	return CELLSStreamActionMempoolHolder.pool
}

type CELLSStreamStateProvider interface {
	AllStreams() []chain.StreamState
	GetStream(streamID string) (chain.StreamState, bool)
	StateRoot() string
}

type CELLSStreamStateProviderHolder struct {
	mu       sync.RWMutex
	provider CELLSStreamStateProvider
}

var CELLSStreamStateProviderGlobal = &CELLSStreamStateProviderHolder{}

// SetStreamStateProvider installs the validator's consensus stream projection.
func SetStreamStateProvider(provider CELLSStreamStateProvider) {
	CELLSStreamStateProviderGlobal.mu.Lock()
	defer CELLSStreamStateProviderGlobal.mu.Unlock()
	CELLSStreamStateProviderGlobal.provider = provider
}

func currentStreamStateProvider() CELLSStreamStateProvider {
	CELLSStreamStateProviderGlobal.mu.RLock()
	defer CELLSStreamStateProviderGlobal.mu.RUnlock()
	return CELLSStreamStateProviderGlobal.provider
}

// CELLSStreamActionEnvelope keeps the wallet signature outside the canonical
// action bytes. The validator reconstructs the exact consensus transaction and
// reuses chain.VerifyStreamActionTx.
type CELLSStreamActionEnvelope struct {
	Action    chain.StreamAction `json:"action"`
	Signature string             `json:"signature"`
	PublicKey string             `json:"public_key"`
}

type CELLSStreamActionSubmitResponse struct {
	ActionID      string `json:"action_id"`
	StreamID      string `json:"stream_id"`
	Action        string `json:"action"`
	Sender        string `json:"sender"`
	Status        string `json:"status"`
	MempoolStatus string `json:"mempool_status"`
}

// CELLSStreamNonceResponse exposes the exact nonce a sender must put in its
// next CELLS/streams/v1 action. This intentionally differs from
// /wallet/nonce.next: wallet transfer envelopes use a one-based wire nonce,
// while consensus contract actions use the account's current expected nonce.
type CELLSStreamNonceResponse struct {
	Runtime     string `json:"runtime"`
	Source      string `json:"source"`
	Sender      string `json:"sender"`
	ActionNonce uint64 `json:"action_nonce"`
	Present     bool   `json:"present"`
}

type CELLSStreamView struct {
	chain.StreamState
	RemainingBudgetDust uint64 `json:"remaining_budget_dust"`
	UnsettledDust       uint64 `json:"unsettled_dust"`
}

type CELLSStreamsResponse struct {
	Runtime   string           `json:"runtime"`
	Source    string           `json:"source"`
	StateRoot string           `json:"state_root"`
	Streams   []CELLSStreamView `json:"streams"`
}

type CELLSStreamResponse struct {
	Runtime   string         `json:"runtime"`
	Source    string         `json:"source"`
	StateRoot string         `json:"state_root"`
	Stream    CELLSStreamView `json:"stream"`
}

func CELLSStreamView(state chain.StreamState) CELLSStreamView {
	unsettled := uint64(0)
	if state.AccruedDust > state.SettledDust {
		unsettled = state.AccruedDust - state.SettledDust
	}
	return CELLSStreamView{
		StreamState:         state,
		RemainingBudgetDust: state.RemainingBudgetDust(),
		UnsettledDust:       unsettled,
	}
}

func CELLSStreamEnvelopeTx(env CELLSStreamActionEnvelope) (*mempool.Tx, error) {
	payload, err := json.Marshal(env.Action)
	if err != nil {
		return nil, err
	}
	return &mempool.Tx{
		ID:         env.Action.ID,
		Sender:     env.Action.Sender,
		Amount:     0,
		Nonce:      env.Action.Nonce,
		Payload:    payload,
		ContractID: chain.StreamContractID,
		Signature:  env.Signature,
		PublicKey:  env.PublicKey,
		AddedAt:    time.Now().UTC(),
	}, nil
}

// CELLSStreamNonceHandler returns the live consensus nonce for stream actions.
// Unknown provider accounts intentionally return nonce 0 with present=false:
// receipt and settle actions may create that zero-balance account atomically.
func (h *Handlers) CELLSStreamNonceHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		writeErrorResponse(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	sender := strings.TrimSpace(r.URL.Query().Get("sender"))
	if sender == "" {
		writeErrorResponse(w, http.StatusBadRequest, "sender query parameter is required")
		return
	}
	if err := ValidateAddress(sender); err != nil {
		writeErrorResponse(w, http.StatusBadRequest, "invalid sender address")
		return
	}
	probe := currentMiningAccountProbe()
	if probe == nil {
		writeErrorResponse(w, http.StatusServiceUnavailable, "consensus account state is not configured")
		return
	}
	_, nonce, present := probe.BalanceOf(sender)
	writeJSONResponse(w, http.StatusOK, CELLSStreamNonceResponse{
		Runtime:     "CELLS-native",
		Source:      "chain",
		Sender:      sender,
		ActionNonce: nonce,
		Present:     present,
	})
}

func (h *Handlers) CELLSStreamActionSubmitSignedHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeErrorResponse(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	pool := currentStreamActionMempool()
	if pool == nil {
		writeErrorResponse(w, http.StatusServiceUnavailable, "CELL stream submission is not configured")
		return
	}

	var env CELLSStreamActionEnvelope
	decoder := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&env); err != nil {
		writeErrorResponse(w, http.StatusBadRequest, "invalid CELL stream envelope: "+SanitizeString(err.Error(), 256))
		return
	}
	tx, err := CELLSStreamEnvelopeTx(env)
	if err != nil {
		writeErrorResponse(w, http.StatusBadRequest, "could not encode CELL stream action")
		return
	}
	if err := chain.VerifyStreamActionTx(tx); err != nil {
		writeErrorResponse(w, http.StatusUnprocessableEntity, SanitizeString(err.Error(), 512))
		return
	}
	probe := currentMiningAccountProbe()
	if probe == nil {
		writeErrorResponse(w, http.StatusServiceUnavailable, "consensus account state is not configured")
		return
	}
	_, expectedNonce, _ := probe.BalanceOf(tx.Sender)
	if tx.Nonce != expectedNonce {
		writeErrorResponse(w, http.StatusUnprocessableEntity, SanitizeString(
			"stale CELL stream action nonce: got "+strconv.FormatUint(tx.Nonce, 10)+
				", current consensus nonce is "+strconv.FormatUint(expectedNonce, 10),
			512,
		))
		return
	}
	status := "submitted"
	if err := pool.Add(tx); err != nil {
		if errors.Is(err, mempool.ErrDuplicateTx) {
			status = "duplicate"
		} else {
			writeErrorResponse(w, http.StatusUnprocessableEntity, SanitizeString(err.Error(), 512))
			return
		}
	}
	writeJSONResponse(w, http.StatusOK, CELLSStreamActionSubmitResponse{
		ActionID:      env.Action.ID,
		StreamID:      env.Action.StreamID,
		Action:        env.Action.Action,
		Sender:        env.Action.Sender,
		Status:        "accepted",
		MempoolStatus: status,
	})
}

func (h *Handlers) CELLSStreamsListHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		writeErrorResponse(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	provider := currentStreamStateProvider()
	if provider == nil {
		writeErrorResponse(w, http.StatusServiceUnavailable, "CELL stream state is not configured")
		return
	}
	payer := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("payer")))
	streamProvider := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("provider")))
	status := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("status")))
	serviceID := strings.TrimSpace(r.URL.Query().Get("service_id"))
	views := make([]CELLSStreamView, 0)
	for _, state := range provider.AllStreams() {
		if payer != "" && !strings.EqualFold(payer, state.Payer) {
			continue
		}
		if streamProvider != "" && !strings.EqualFold(streamProvider, state.Provider) {
			continue
		}
		if status != "" && !strings.EqualFold(status, state.Status) {
			continue
		}
		if serviceID != "" && state.ServiceID != serviceID {
			continue
		}
		views = append(views, CELLSStreamView(state))
	}
	writeJSONResponse(w, http.StatusOK, CELLSStreamsResponse{
		Runtime:   "CELLS-native",
		Source:    "chain",
		StateRoot: provider.StateRoot(),
		Streams:   views,
	})
}

func (h *Handlers) CELLSStreamRouteHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		writeErrorResponse(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	provider := currentStreamStateProvider()
	if provider == nil {
		writeErrorResponse(w, http.StatusServiceUnavailable, "CELL stream state is not configured")
		return
	}
	streamID := strings.TrimPrefix(r.URL.Path, "/api/v1/streams/")
	streamID = strings.TrimSpace(strings.Trim(streamID, "/"))
	if streamID == "" || streamID == "actions" || strings.HasPrefix(streamID, "actions/") {
		writeErrorResponse(w, http.StatusBadRequest, "stream_id required")
		return
	}
	decoded, err := url.PathUnescape(streamID)
	if err != nil || decoded == "" || len(decoded) > 128 {
		writeErrorResponse(w, http.StatusBadRequest, "invalid stream_id")
		return
	}
	state, ok := provider.GetStream(decoded)
	if !ok {
		writeErrorResponse(w, http.StatusNotFound, "CELL stream not found")
		return
	}
	writeJSONResponse(w, http.StatusOK, CELLSStreamResponse{
		Runtime:   "CELLS-native",
		Source:    "chain",
		StateRoot: provider.StateRoot(),
		Stream:    CELLSStreamView(state),
	})
}
