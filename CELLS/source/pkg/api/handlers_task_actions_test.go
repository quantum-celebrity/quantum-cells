package api

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/blackbeardONE/CELLS/pkg/chain"
	"github.com/blackbeardONE/CELLS/pkg/wallet"
)

func buildSignedTaskAction(t *testing.T, ws *wallet.WalletService, taskID, action string) CELLSTaskActionEnvelope {
	t.Helper()
	pubKey := ws.GetPublicKey()
	if pubKey == nil {
		t.Fatal("ws.GetPublicKey returned nil")
	}
	addrHash := sha256.Sum256(pubKey)
	sender := hex.EncodeToString(addrHash[:])
	idHash := sha256.Sum256([]byte(sender + taskID + action + time.Now().UTC().Format(time.RFC3339Nano)))

	env := CELLSTaskActionEnvelope{
		ID:        hex.EncodeToString(idHash[:16]),
		Sender:    sender,
		TaskID:    taskID,
		Action:    action,
		Payload:   `{"note":"test"}`,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}
	signTaskActionEnvelope(t, ws, &env)
	return env
}

func TestValidateCELLSTaskActionEnvelope_AllowsCatalogActions(t *testing.T) {
	for _, action := range []string{
		chain.TaskActionCatalogRegister,
		chain.TaskActionCatalogUpdate,
		chain.TaskActionCatalogPause,
		chain.TaskActionCatalogResume,
	} {
		t.Run(action, func(t *testing.T) {
			err := validateCELLSTaskActionEnvelope(CELLSTaskActionEnvelope{
				ID:        strings.Repeat("a", 32),
				Sender:    strings.Repeat("b", 64),
				TaskID:    "catalog-task",
				Action:    action,
				Payload:   `{}`,
				Timestamp: time.Now().UTC().Format(time.RFC3339),
				PublicKey: "01",
				Signature: "02",
			})
			if err != nil {
				t.Fatalf("catalog action rejected: %v", err)
			}
		})
	}
}

func signTaskActionEnvelope(t *testing.T, ws *wallet.WalletService, env *CELLSTaskActionEnvelope) {
	t.Helper()
	pubKey := ws.GetPublicKey()
	if pubKey == nil {
		t.Fatal("ws.GetPublicKey returned nil")
	}
	env.Signature = ""
	env.PublicKey = ""
	canonical, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("marshal canonical task action: %v", err)
	}
	sig, err := ws.SignData(canonical)
	if err != nil {
		t.Fatalf("sign task action: %v", err)
	}
	env.Signature = hex.EncodeToString(sig)
	env.PublicKey = hex.EncodeToString(pubKey)
}

func postTaskAction(t *testing.T, h *Handlers, env CELLSTaskActionEnvelope) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("marshal task action: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks/actions/submit-signed", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.CELLSTaskActionSubmitSignedHandler(rec, req)
	return rec
}

func TestCELLSTaskActionSubmitSigned_HappyPath(t *testing.T) {
	ws, err := wallet.NewWalletService()
	if err != nil {
		t.Skipf("wallet requires Dilithium: %v", err)
	}
	actionLog := filepath.Join(t.TempDir(), "task-actions.jsonl")
	t.Setenv(CELLSTaskActionLogPathEnv, actionLog)
	SetTaskActionMempool(&fakeSubmitter{})
	t.Cleanup(func() { SetTaskActionMempool(nil) })
	h := setupTestHandlersWithSubmesh(nil, ws)

	env := buildSignedTaskAction(t, ws, "task-1", "start")
	rec := postTaskAction(t, h, env)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var resp CELLSTaskActionSubmitResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.ActionID != env.ID || resp.Status != "accepted" || resp.Action != "start" {
		t.Fatalf("unexpected response: %+v", resp)
	}
	rawLog, err := os.ReadFile(actionLog)
	if err != nil {
		t.Fatalf("read action log: %v", err)
	}
	if !strings.Contains(string(rawLog), env.ID) {
		t.Fatalf("action log missing id %q: %s", env.ID, string(rawLog))
	}
}

func TestCELLSTaskActionSubmitSigned_RejectsWhenMempoolNotConfigured(t *testing.T) {
	ws, err := wallet.NewWalletService()
	if err != nil {
		t.Skipf("wallet requires Dilithium: %v", err)
	}
	t.Setenv(CELLSTaskActionLogPathEnv, filepath.Join(t.TempDir(), "task-actions.jsonl"))
	SetTaskActionMempool(nil)
	h := setupTestHandlersWithSubmesh(nil, ws)

	env := buildSignedTaskAction(t, ws, "task-1", "start")
	rec := postTaskAction(t, h, env)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503; body=%s", rec.Code, rec.Body.String())
	}
	var resp CELLSTaskActionSubmitResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Status != "rejected" || resp.MempoolStatus != "not_configured" {
		t.Fatalf("unexpected response: %+v", resp)
	}
}

func TestTaskActionMempoolReady(t *testing.T) {
	SetTaskActionMempool(nil)
	if TaskActionMempoolReady() {
		t.Fatal("TaskActionMempoolReady = true without a configured pool")
	}

	SetTaskActionMempool(&fakeSubmitter{})
	t.Cleanup(func() { SetTaskActionMempool(nil) })
	if !TaskActionMempoolReady() {
		t.Fatal("TaskActionMempoolReady = false with a configured pool")
	}
}

func TestCELLSTaskActionSubmitSigned_SubmitsToMempoolWhenConfigured(t *testing.T) {
	ws, err := wallet.NewWalletService()
	if err != nil {
		t.Skipf("wallet requires Dilithium: %v", err)
	}
	actionLog := filepath.Join(t.TempDir(), "task-actions.jsonl")
	t.Setenv(CELLSTaskActionLogPathEnv, actionLog)
	pool := &fakeSubmitter{}
	SetTaskActionMempool(pool)
	t.Cleanup(func() { SetTaskActionMempool(nil) })
	h := setupTestHandlersWithSubmesh(nil, ws)

	env := buildSignedTaskAction(t, ws, "task-1", "stake")
	env.Amount = 3
	env.Nonce = 7
	signTaskActionEnvelope(t, ws, &env)
	rec := postTaskAction(t, h, env)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var resp CELLSTaskActionSubmitResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !resp.MempoolSubmitted || resp.MempoolStatus != "submitted" {
		t.Fatalf("unexpected mempool response: %+v", resp)
	}
	if len(pool.added) != 1 {
		t.Fatalf("mempool added %d txs, want 1", len(pool.added))
	}
	tx := pool.added[0]
	if tx.ID != env.ID || tx.Sender != env.Sender || tx.Nonce != env.Nonce || tx.ContractID != chain.TaskContractID {
		t.Fatalf("unexpected task action tx: %+v", tx)
	}
	if tx.Amount != env.Amount {
		t.Fatalf("task stake tx amount: got %v, want %v", tx.Amount, env.Amount)
	}
	var action chain.TaskAction
	if err := json.Unmarshal(tx.Payload, &action); err != nil {
		t.Fatalf("decode task action payload: %v", err)
	}
	if action.ID != env.ID || action.TaskID != env.TaskID || action.Action != env.Action || action.Amount != env.Amount {
		t.Fatalf("unexpected task action payload: %+v", action)
	}
}

func TestCELLSTaskActionSubmitSigned_Duplicate(t *testing.T) {
	ws, err := wallet.NewWalletService()
	if err != nil {
		t.Skipf("wallet requires Dilithium: %v", err)
	}
	t.Setenv(CELLSTaskActionLogPathEnv, filepath.Join(t.TempDir(), "task-actions.jsonl"))
	SetTaskActionMempool(&fakeSubmitter{})
	t.Cleanup(func() { SetTaskActionMempool(nil) })
	h := setupTestHandlersWithSubmesh(nil, ws)
	env := buildSignedTaskAction(t, ws, "task-1", "stop")

	first := postTaskAction(t, h, env)
	if first.Code != http.StatusOK {
		t.Fatalf("first status = %d, want 200; body=%s", first.Code, first.Body.String())
	}
	second := postTaskAction(t, h, env)
	if second.Code != http.StatusConflict {
		t.Fatalf("second status = %d, want 409; body=%s", second.Code, second.Body.String())
	}
	var resp CELLSTaskActionSubmitResponse
	if err := json.Unmarshal(second.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode duplicate response: %v", err)
	}
	if resp.Status != "duplicate" {
		t.Fatalf("status = %q, want duplicate", resp.Status)
	}
}

func TestCELLSTaskActionSubmitSigned_DoesNotUseActionLogForNonceReplay(t *testing.T) {
	ws, err := wallet.NewWalletService()
	if err != nil {
		t.Skipf("wallet requires Dilithium: %v", err)
	}
	t.Setenv(CELLSTaskActionLogPathEnv, filepath.Join(t.TempDir(), "task-actions.jsonl"))
	pool := &fakeSubmitter{}
	SetTaskActionMempool(pool)
	t.Cleanup(func() { SetTaskActionMempool(nil) })
	h := setupTestHandlersWithSubmesh(nil, ws)

	firstEnv := buildSignedTaskAction(t, ws, "task-1", "start")
	firstEnv.Nonce = 37
	signTaskActionEnvelope(t, ws, &firstEnv)
	if first := postTaskAction(t, h, firstEnv); first.Code != http.StatusOK {
		t.Fatalf("first status = %d, want 200; body=%s", first.Code, first.Body.String())
	}

	replayEnv := buildSignedTaskAction(t, ws, "task-1", "stop")
	replayEnv.Nonce = 8
	signTaskActionEnvelope(t, ws, &replayEnv)
	second := postTaskAction(t, h, replayEnv)
	if second.Code != http.StatusOK {
		t.Fatalf("second status = %d, want 200; body=%s", second.Code, second.Body.String())
	}
	var resp CELLSTaskActionSubmitResponse
	if err := json.Unmarshal(second.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Status != "accepted" {
		t.Fatalf("unexpected response: %+v", resp)
	}
	if len(pool.added) != 2 {
		t.Fatalf("mempool added %d txs, want 2", len(pool.added))
	}
}

func TestCELLSTaskActionSubmitSigned_SenderMismatch(t *testing.T) {
	ws, err := wallet.NewWalletService()
	if err != nil {
		t.Skipf("wallet requires Dilithium: %v", err)
	}
	t.Setenv(CELLSTaskActionLogPathEnv, filepath.Join(t.TempDir(), "task-actions.jsonl"))
	h := setupTestHandlersWithSubmesh(nil, ws)
	env := buildSignedTaskAction(t, ws, "task-1", "claim")
	env.Sender = strings.Repeat("a", 64)

	rec := postTaskAction(t, h, env)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "sender does not match") {
		t.Fatalf("expected sender mismatch body, got %s", rec.Body.String())
	}
}

func TestCELLSTaskActionSubmitSigned_BadSignature(t *testing.T) {
	ws, err := wallet.NewWalletService()
	if err != nil {
		t.Skipf("wallet requires Dilithium: %v", err)
	}
	t.Setenv(CELLSTaskActionLogPathEnv, filepath.Join(t.TempDir(), "task-actions.jsonl"))
	h := setupTestHandlersWithSubmesh(nil, ws)
	env := buildSignedTaskAction(t, ws, "task-1", "submit")
	if env.Signature[0] == '0' {
		env.Signature = "1" + env.Signature[1:]
	} else {
		env.Signature = "0" + env.Signature[1:]
	}

	rec := postTaskAction(t, h, env)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body=%s", rec.Code, rec.Body.String())
	}
}

func TestCELLSTaskActionSubmitSigned_Unconfigured(t *testing.T) {
	ws, err := wallet.NewWalletService()
	if err != nil {
		t.Skipf("wallet requires Dilithium: %v", err)
	}
	t.Setenv(CELLSTaskActionLogPathEnv, "")
	h := setupTestHandlersWithSubmesh(nil, ws)
	env := buildSignedTaskAction(t, ws, "task-1", "start")

	rec := postTaskAction(t, h, env)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503; body=%s", rec.Code, rec.Body.String())
	}
}

func TestCELLSTaskActionsListHandler_FiltersByTask(t *testing.T) {
	ws, err := wallet.NewWalletService()
	if err != nil {
		t.Skipf("wallet requires Dilithium: %v", err)
	}
	t.Setenv(CELLSTaskActionLogPathEnv, filepath.Join(t.TempDir(), "task-actions.jsonl"))
	SetTaskActionMempool(&fakeSubmitter{})
	t.Cleanup(func() { SetTaskActionMempool(nil) })
	h := setupTestHandlersWithSubmesh(nil, ws)
	envA := buildSignedTaskAction(t, ws, "task-a", "start")
	envB := buildSignedTaskAction(t, ws, "task-b", "start")
	if rec := postTaskAction(t, h, envA); rec.Code != http.StatusOK {
		t.Fatalf("post A status = %d body=%s", rec.Code, rec.Body.String())
	}
	if rec := postTaskAction(t, h, envB); rec.Code != http.StatusOK {
		t.Fatalf("post B status = %d body=%s", rec.Code, rec.Body.String())
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks/actions?task_id=task-b", nil)
	rec := httptest.NewRecorder()
	h.CELLSTaskActionsListHandler(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var resp CELLSTaskActionsListResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	if len(resp.Actions) != 1 || resp.Actions[0].Envelope.TaskID != "task-b" {
		t.Fatalf("unexpected actions: %+v", resp.Actions)
	}
}
