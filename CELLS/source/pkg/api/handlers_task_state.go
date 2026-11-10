package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"

	"github.com/blackbeardONE/CELLS/pkg/chain"
)

type CELLSTaskStatesResponse struct {
	Runtime    string            `json:"runtime"`
	Configured bool              `json:"configured"`
	Source     string            `json:"source,omitempty"`
	StateRoot  string            `json:"state_root"`
	Tasks      []chain.TaskState `json:"tasks"`
}

type CELLSTaskStateResponse struct {
	Runtime    string          `json:"runtime"`
	Configured bool            `json:"configured"`
	Source     string          `json:"source,omitempty"`
	StateRoot  string          `json:"state_root"`
	Task       chain.TaskState `json:"task"`
}

type CELLSTaskStateProvider interface {
	AllTasks() []chain.TaskState
	GetTask(taskID string) (chain.TaskState, bool)
	StateRoot() string
}

const (
	CELLSTaskChainSource   = "chain"
	CELLSTaskOverlaySource = "operator-overlay"
)

type CELLSTaskStateProviderHolder struct {
	mu       sync.RWMutex
	provider CELLSTaskStateProvider
}

var CELLSTaskStateProviderGlobal = &CELLSTaskStateProviderHolder{}

func SetTaskStateProvider(provider CELLSTaskStateProvider) {
	CELLSTaskStateProviderGlobal.mu.Lock()
	defer CELLSTaskStateProviderGlobal.mu.Unlock()
	CELLSTaskStateProviderGlobal.provider = provider
}

func currentTaskStateProvider() CELLSTaskStateProvider {
	CELLSTaskStateProviderGlobal.mu.RLock()
	defer CELLSTaskStateProviderGlobal.mu.RUnlock()
	return CELLSTaskStateProviderGlobal.provider
}

func loadCELLSTaskActionProjection(path string) (*chain.TaskStateStore, error) {
	store := chain.NewTaskStateStore()
	if path == "" {
		return store, nil
	}
	actions, err := readCELLSTaskActions(path, "", "", -1)
	if err != nil {
		return nil, err
	}
	for _, record := range actions {
		if err := store.ApplyAction(CELLSTaskActionToChain(record.Envelope)); err != nil {
			if isIgnorableTaskActionProjectionError(err) {
				continue
			}
			return nil, err
		}
	}
	return store, nil
}

func loadCELLSTaskActionStateStore() (CELLSTaskStateProvider, string, bool, error) {
	path := CELLSTaskActionLogPath()
	if provider := currentTaskStateProvider(); provider != nil {
		// The live consensus store is authoritative. The action log is an audit
		// trail and can contain unconfirmed actions plus legacy client units and
		// proof shapes, so merging it into current state can display balances the
		// chain never accepted or make the endpoint unavailable after upgrades.
		return provider, CELLSTaskChainSource, true, nil
	}

	if path == "" {
		return chain.NewTaskStateStore(), "", false, nil
	}
	store, err := loadCELLSTaskActionProjection(path)
	if err != nil {
		return nil, CELLSTaskOverlaySource, true, err
	}
	return store, CELLSTaskOverlaySource, true, nil
}

type CELLSTaskProjectionResult struct {
	Tasks      []CELLSTask
	Source     string
	StateRoot  string
	Configured bool
}

func applyCELLSTaskActionProjection(tasks []CELLSTask) (CELLSTaskProjectionResult, error) {
	store, source, configured, err := loadCELLSTaskActionStateStore()
	projectedTasks := make([]CELLSTask, len(tasks))
	copy(projectedTasks, tasks)
	result := CELLSTaskProjectionResult{
		Tasks:      projectedTasks,
		Source:     source,
		Configured: configured,
	}
	if err != nil || !configured {
		return result, err
	}
	result.StateRoot = chain.TaskCatalogRoot(store.AllTasks())

	indexByID := make(map[string]int, len(result.Tasks))
	for i := range result.Tasks {
		indexByID[result.Tasks[i].TaskID] = i
	}
	for _, state := range store.AllTasks() {
		if state.Manifest == nil {
			continue
		}
		if index, ok := indexByID[state.TaskID]; ok {
			result.Tasks[index] = applyCELLSTaskManifest(result.Tasks[index], state)
			continue
		}
		result.Tasks = append(result.Tasks, applyCELLSTaskManifest(CELLSTask{}, state))
		indexByID[state.TaskID] = len(result.Tasks) - 1
	}

	for i := range result.Tasks {
		state, ok := store.GetTask(result.Tasks[i].TaskID)
		if !ok {
			continue
		}
		if state.Manifest != nil {
			result.Tasks[i] = applyCELLSTaskManifest(result.Tasks[i], state)
		}

		// Consensus state is authoritative. Rebuilding these maps prevents stale
		// bootstrap JSON values from being added to live stake and reward totals.
		result.Tasks[i].StakeList = map[string]float64{}
		result.Tasks[i].AvailableBalances = map[string]float64{}
		for sender, participant := range state.Participants {
			if participant.Stake > 0 {
				result.Tasks[i].StakeList[sender] = participant.Stake
			}
			if participant.PendingRewardAmount > 0 {
				result.Tasks[i].AvailableBalances[sender] = participant.PendingRewardAmount
			}
		}
		result.Tasks[i].TotalStakeAmount = state.TotalStakeAmount
		result.Tasks[i].RewardPoolAmount = state.RewardPoolAmount
		result.Tasks[i].PendingRewardAmount = state.PendingRewardAmount
		result.Tasks[i].TotalRewardPaidAmount = state.TotalRewardPaidAmount
		result.Tasks[i].Submissions = map[string]map[string]CELLSTaskSubmission{}
		result.Tasks[i].DistributionRewardsSubmission = map[string]map[string]CELLSTaskSubmission{}
		for round, bySender := range state.Submissions {
			result.Tasks[i].Submissions[round] = map[string]CELLSTaskSubmission{}
			result.Tasks[i].DistributionRewardsSubmission[round] = map[string]CELLSTaskSubmission{}
			for sender, submission := range bySender {
				projectedSubmission := CELLSTaskSubmission{
					SubmissionValue: submission.SubmissionValue,
					Slot:            submission.Slot,
					RewardAmount:    submission.RewardAmount,
					Claimed:         submission.Claimed,
					ClaimedAt:       submission.ClaimedAt,
				}
				result.Tasks[i].Submissions[round][sender] = projectedSubmission
				if submission.RewardAmount > 0 {
					result.Tasks[i].DistributionRewardsSubmission[round][sender] = projectedSubmission
				}
			}
		}
		if state.IsMigrated {
			result.Tasks[i].IsMigrated = true
			result.Tasks[i].MigratedTo = state.MigratedTo
		}
	}
	return result, nil
}

func applyCELLSTaskManifest(task CELLSTask, state chain.TaskState) CELLSTask {
	manifest := state.Manifest
	if manifest == nil {
		return task
	}

	task.TaskID = manifest.TaskID
	task.TaskName = manifest.Name
	task.TaskManager = manifest.Manager
	task.IsActive = manifest.Active && !state.CatalogPaused
	task.TaskDescription = manifest.Description
	task.MinimumStakeAmount = manifest.MinimumStakeAmount
	task.BountyAmountPerRound = manifest.RewardPerRound
	task.RoundTime = manifest.RoundTime
	task.SubmissionWindow = manifest.SubmissionWindow
	task.AuditWindow = manifest.AuditWindow
	task.TaskMetadata = manifest.MetadataURL
	task.TaskType = "CELL"
	task.TokenType = "CELL"
	task.CatalogVersion = manifest.Version
	task.CatalogPaused = state.CatalogPaused
	task.CatalogPublishedAt = state.CatalogPublishedAt
	task.CatalogUpdatedAt = state.CatalogUpdatedAt
	manifestCopy := *manifest
	manifestCopy.Tags = append([]string(nil), manifest.Tags...)
	task.Manifest = &manifestCopy

	runtimeJSON, err := json.Marshal(manifest.Runtime)
	if err == nil {
		task.TaskVars = string(runtimeJSON)
	}
	switch manifest.Runtime.Kind {
	case "capability":
		task.TaskExecutableNetwork = "CELLS-CAPABILITY"
		task.TaskAuditProgram = manifest.Runtime.Capability
		task.NativeRuntime = "CELLS-capability"
	case "wasm":
		task.TaskExecutableNetwork = "CELLS-WASM"
		task.TaskAuditProgram = manifest.Runtime.ModuleSHA256
		task.NativeRuntime = "CELLS-wasm"
	}
	task = normalizeCELLSTask(task)
	if strings.EqualFold(manifest.Runtime.Kind, "capability") {
		task.NativeRuntime = "CELLS-capability"
	} else if strings.EqualFold(manifest.Runtime.Kind, "wasm") {
		task.NativeRuntime = "CELLS-wasm"
	}
	return task
}

func (h *Handlers) CELLSTaskStatesHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		writeErrorResponse(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	store, path, configured, err := loadCELLSTaskActionStateStore()
	if err != nil {
		writeErrorResponse(w, http.StatusInternalServerError, "failed to project task action state: "+err.Error())
		return
	}
	writeJSONResponse(w, http.StatusOK, CELLSTaskStatesResponse{
		Runtime:    "CELLS-native",
		Configured: configured,
		Source:     path,
		StateRoot:  store.StateRoot(),
		Tasks:      store.AllTasks(),
	})
}
