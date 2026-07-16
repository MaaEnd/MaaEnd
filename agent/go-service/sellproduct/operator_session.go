package sellproduct

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const (
	operatorSessionOperationReset           = "reset"
	operatorSessionOperationRegister        = "register"
	operatorSessionOperationCompleteRestore = "complete_restore"
	operatorSessionOperationSkipRestore     = "skip_restore"
)

// operatorSessionState 保存一次 SellProduct 任务内的自动干员状态。
// ActiveLocations 在任务入口一次性注册，避免恢复算法把干员预留给未启用据点；
// CompletedRestoreLocations 排除已恢复或已确认无法恢复的据点；
// LockedRestoreAssignments 则固定成功恢复的结果，后续重新规划不能挪用这些干员。
type operatorSessionState struct {
	UID                       string
	Mode                      string
	ActiveLocations           map[string]struct{}
	CompletedRestoreLocations map[string]struct{}
	PlannedRestoreAssignments map[string]operatorCandidate
	LockedRestoreAssignments  map[string]operatorCandidate
	RetriedSelections         map[string]struct{}
	Refreshed                 bool
}

type operatorSessionActionParam struct {
	Operation string `json:"operation"`
	Mode      string `json:"mode,omitempty"`
	Location  string `json:"location,omitempty"`
}

// OperatorSessionAction 由 Pipeline 在任务入口和恢复完成节点调用。
// Go 只维护算法所需会话数据，节点启用范围和调用顺序仍由 Pipeline 决定。
type OperatorSessionAction struct{}

var _ maa.CustomActionRunner = (*OperatorSessionAction)(nil)

var (
	operatorStateMu sync.Mutex
	operatorSession operatorSessionState
)

func (a *OperatorSessionAction) Run(_ *maa.Context, arg *maa.CustomActionArg) bool {
	p, err := parseOperatorSessionActionParam(arg)
	if err != nil {
		log.Error().Err(err).Str("component", operatorSessionActionName).Msg("invalid params")
		return false
	}

	switch p.Operation {
	case operatorSessionOperationReset:
		operatorSessionReset(p.Mode)
	case operatorSessionOperationRegister:
		operatorSessionRegisterLocation(p.Location)
	case operatorSessionOperationCompleteRestore:
		if !operatorSessionCompleteRestore(p.Location) {
			log.Error().Str("component", operatorSessionActionName).Str("location", p.Location).
				Msg("restore completed without a planned assignment")
			return false
		}
	case operatorSessionOperationSkipRestore:
		operatorSessionSkipRestore(p.Location)
	default:
		log.Error().Str("component", operatorSessionActionName).Str("operation", p.Operation).
			Msg("unsupported operation")
		return false
	}
	return true
}

func parseOperatorSessionActionParam(arg *maa.CustomActionArg) (*operatorSessionActionParam, error) {
	if arg == nil || strings.TrimSpace(arg.CustomActionParam) == "" {
		return nil, fmt.Errorf("custom_action_param is empty")
	}
	var p operatorSessionActionParam
	if err := json.Unmarshal([]byte(arg.CustomActionParam), &p); err != nil {
		return nil, fmt.Errorf("unmarshal custom_action_param: %w", err)
	}
	p.Operation = strings.TrimSpace(p.Operation)
	p.Mode = strings.TrimSpace(p.Mode)
	p.Location = strings.TrimSpace(p.Location)
	if p.Operation == operatorSessionOperationReset {
		if p.Mode == "" {
			p.Mode = operatorCacheModeCache
		}
		if p.Mode != operatorCacheModeCache && p.Mode != operatorCacheModeRefresh {
			return nil, fmt.Errorf("invalid mode %q", p.Mode)
		}
	}
	if (p.Operation == operatorSessionOperationRegister ||
		p.Operation == operatorSessionOperationCompleteRestore ||
		p.Operation == operatorSessionOperationSkipRestore) &&
		p.Location == "" {
		return nil, fmt.Errorf("location is empty")
	}
	return &p, nil
}

func operatorSessionReset(mode string) {
	operatorStateMu.Lock()
	defer operatorStateMu.Unlock()
	operatorSession = operatorSessionState{
		UID:                       currentOperatorCacheUID(),
		Mode:                      mode,
		ActiveLocations:           map[string]struct{}{},
		CompletedRestoreLocations: map[string]struct{}{},
		PlannedRestoreAssignments: map[string]operatorCandidate{},
		LockedRestoreAssignments:  map[string]operatorCandidate{},
		RetriedSelections:         map[string]struct{}{},
	}
	operatorListScanStates = map[string]operatorListScanState{}
}

func operatorSessionRegisterLocation(location string) {
	operatorStateMu.Lock()
	defer operatorStateMu.Unlock()
	ensureOperatorSessionLocked()
	operatorSession.ActiveLocations[location] = struct{}{}
}

func operatorSessionSnapshot() operatorSessionState {
	operatorStateMu.Lock()
	defer operatorStateMu.Unlock()
	ensureOperatorSessionLocked()
	return operatorSessionState{
		UID:                       operatorSession.UID,
		Mode:                      operatorSession.Mode,
		ActiveLocations:           cloneStringSet(operatorSession.ActiveLocations),
		CompletedRestoreLocations: cloneStringSet(operatorSession.CompletedRestoreLocations),
		PlannedRestoreAssignments: cloneRestoreAssignments(operatorSession.PlannedRestoreAssignments),
		LockedRestoreAssignments:  cloneRestoreAssignments(operatorSession.LockedRestoreAssignments),
		RetriedSelections:         cloneStringSet(operatorSession.RetriedSelections),
		Refreshed:                 operatorSession.Refreshed,
	}
}

func operatorSessionSetPlannedRestore(location string, candidate operatorCandidate, ok bool) {
	operatorStateMu.Lock()
	defer operatorStateMu.Unlock()
	ensureOperatorSessionLocked()
	if !ok {
		delete(operatorSession.PlannedRestoreAssignments, location)
		return
	}
	operatorSession.PlannedRestoreAssignments[location] = candidate
}

func operatorSessionCompleteRestore(location string) bool {
	operatorStateMu.Lock()
	defer operatorStateMu.Unlock()
	ensureOperatorSessionLocked()
	candidate, ok := operatorSession.PlannedRestoreAssignments[location]
	if !ok {
		return false
	}
	operatorSession.LockedRestoreAssignments[location] = candidate
	operatorSession.CompletedRestoreLocations[location] = struct{}{}
	delete(operatorSession.PlannedRestoreAssignments, location)
	return true
}

// operatorSessionSkipRestore 记录当前据点已经确认没有可用恢复干员。
// 后续重新规划必须排除该据点，避免继续为它预留其他据点需要的共享干员。
func operatorSessionSkipRestore(location string) {
	operatorStateMu.Lock()
	defer operatorStateMu.Unlock()
	ensureOperatorSessionLocked()
	operatorSession.CompletedRestoreLocations[location] = struct{}{}
	delete(operatorSession.PlannedRestoreAssignments, location)
}

func operatorSessionMarkRefreshed() {
	operatorStateMu.Lock()
	defer operatorStateMu.Unlock()
	ensureOperatorSessionLocked()
	operatorSession.Refreshed = true
}

// operatorSessionClaimRetry 保证同一用途、同一据点在一次任务中最多执行一次重新选择。
func operatorSessionClaimRetry(usage string, location string) bool {
	operatorStateMu.Lock()
	defer operatorStateMu.Unlock()
	ensureOperatorSessionLocked()
	key := strings.Join([]string{usage, location}, "|")
	if _, exists := operatorSession.RetriedSelections[key]; exists {
		return false
	}
	operatorSession.RetriedSelections[key] = struct{}{}
	return true
}

func operatorSessionRefreshed() bool {
	operatorStateMu.Lock()
	defer operatorStateMu.Unlock()
	ensureOperatorSessionLocked()
	return operatorSession.Refreshed
}

func ensureOperatorSessionLocked() {
	uid := currentOperatorCacheUID()
	if operatorSession.UID == uid && operatorSession.ActiveLocations != nil {
		return
	}
	operatorSession = operatorSessionState{
		UID:                       uid,
		Mode:                      operatorCacheModeCache,
		ActiveLocations:           map[string]struct{}{},
		CompletedRestoreLocations: map[string]struct{}{},
		PlannedRestoreAssignments: map[string]operatorCandidate{},
		LockedRestoreAssignments:  map[string]operatorCandidate{},
		RetriedSelections:         map[string]struct{}{},
	}
}

func cloneStringSet(src map[string]struct{}) map[string]struct{} {
	dst := make(map[string]struct{}, len(src))
	for value := range src {
		dst[value] = struct{}{}
	}
	return dst
}

func operatorListStateGet(key string) (operatorListScanState, bool) {
	operatorStateMu.Lock()
	defer operatorStateMu.Unlock()
	state, ok := operatorListScanStates[key]
	return state, ok
}

func operatorListStateSet(state operatorListScanState) {
	operatorStateMu.Lock()
	defer operatorStateMu.Unlock()
	operatorListScanStates[state.Key] = state
}

func operatorListStateDelete(key string) {
	operatorStateMu.Lock()
	defer operatorStateMu.Unlock()
	delete(operatorListScanStates, key)
}
