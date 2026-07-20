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
	operatorSessionOperationEnterLocation   = "enter_location"
	operatorSessionOperationCompleteTarget  = "complete_target"
	operatorSessionOperationCompleteRestore = "complete_restore"
	operatorSessionOperationSkipRestore     = "skip_restore"
	operatorSessionOperationExcludeSelected = "exclude_selected"
)

// operatorSessionState 保存一次 SellProduct 任务内的自动干员状态。
// ActiveLocations 在任务入口一次性注册，避免恢复算法把干员预留给未启用据点；
// CompletedRestoreLocations 排除已恢复或已确认无法恢复的据点；
// TargetAssignments 记录各据点本轮实际使用的售卖干员，供恢复规划减少无收益切换；
// LockedRestoreAssignments 则固定成功恢复的结果，后续重新规划不能挪用这些干员；
// ExcludedOperators 临时排除已在其他据点派驻且不应被抢占的干员；
// OutpostProsperityMaxLocations 记录据点发展值已达上限的据点，供售卖候选忽略发展值加成。
type operatorSessionState struct {
	UID                           string
	Mode                          string
	ActiveLocations               map[string]struct{}
	CompletedRestoreLocations     map[string]struct{}
	EnteredLocations              map[string]struct{}
	TargetAssignments             map[string]operatorCandidate
	PlannedRestoreAssignments     map[string]operatorCandidate
	LockedRestoreAssignments      map[string]operatorCandidate
	ExcludedOperators             map[string]struct{}
	OutpostProsperityMaxLocations map[string]struct{}
	RetriedSelections             map[string]struct{}
	CacheNoticePrinted            bool
	Refreshed                     bool
}

type operatorSessionActionParam struct {
	Operation            string `json:"operation"`
	Mode                 string `json:"mode,omitempty"`
	Usage                string `json:"usage,omitempty"`
	Location             string `json:"location,omitempty"`
	Changed              bool   `json:"changed,omitempty"`
	Active               bool   `json:"active,omitempty"`
	OutpostProsperityMax bool   `json:"outpost_prosperity_max,omitempty"`
}

// OperatorSessionAction 由 Pipeline 在任务入口和恢复完成节点调用。
// Go 只维护算法所需会话数据，节点启用范围和调用顺序仍由 Pipeline 决定。
type OperatorSessionAction struct{}

var _ maa.CustomActionRunner = (*OperatorSessionAction)(nil)

var (
	operatorStateMu sync.Mutex
	operatorSession operatorSessionState
)

func (a *OperatorSessionAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	p, err := parseOperatorSessionActionParam(arg)
	if err != nil {
		log.Error().Err(err).Str("component", operatorSessionActionName).Msg("invalid params")
		return false
	}

	switch p.Operation {
	case operatorSessionOperationReset:
		operatorSessionReset(p.Mode)
	case operatorSessionOperationRegister:
		if !p.Active {
			log.Debug().Str("component", operatorSessionActionName).
				Str("location", p.Location).
				Msg("inactive operator location slot skipped")
			return true
		}
		operatorSessionRegisterLocation(p.Location)
	case operatorSessionOperationEnterLocation:
		uid := operatorSessionSetOutpostProsperityMax(p.Location, p.OutpostProsperityMax)
		if _, err := persistOutpostProsperityStatus(uid, p.Location, p.OutpostProsperityMax); err != nil {
			log.Warn().Err(err).
				Str("component", operatorSessionActionName).
				Str("location", p.Location).
				Bool("outpost_prosperity_max", p.OutpostProsperityMax).
				Msg("failed to persist outpost prosperity status")
		}
		if operatorSessionEnterLocation(p.Location) {
			if err := printRuntimeLocationPlan(ctx, p.Location); err != nil {
				log.Warn().Err(err).
					Str("component", operatorSessionActionName).
					Str("location", p.Location).
					Msg("failed to print outpost plan")
				printRuntimeLocationEntered(ctx, p.Location)
			}
		}
	case operatorSessionOperationCompleteTarget:
		candidate, ok := operatorSessionTargetAssignment(p.Location)
		if !ok {
			log.Error().Str("component", operatorSessionActionName).Str("location", p.Location).
				Msg("target completed without a planned assignment")
			return false
		}
		printRuntimeOperatorAssignment(ctx, p.Location, operatorActionUsageTarget, candidate, p.Changed)
	case operatorSessionOperationCompleteRestore:
		candidate, ok := operatorSessionCompleteRestore(p.Location)
		if !ok {
			log.Error().Str("component", operatorSessionActionName).Str("location", p.Location).
				Msg("restore completed without a planned assignment")
			return false
		}
		printRuntimeOperatorAssignment(ctx, p.Location, operatorActionUsageRestore, candidate, p.Changed)
	case operatorSessionOperationSkipRestore:
		operatorSessionSkipRestore(p.Location)
		printRuntimeRestoreSkipped(ctx, p.Location)
	case operatorSessionOperationExcludeSelected:
		candidate, ok := operatorSessionExcludeSelected(p.Usage, p.Location)
		if !ok {
			log.Error().Str("component", operatorSessionActionName).
				Str("usage", p.Usage).
				Str("location", p.Location).
				Msg("operator exclusion has no selected candidate")
			return false
		}
		log.Warn().Str("component", operatorSessionActionName).
			Str("usage", p.Usage).
			Str("location", p.Location).
			Str("operator", candidate.Name).
			Msg("operator excluded after already assigned prompt")
		printRuntimeOperatorConflict(ctx, p.Location, p.Usage, candidate)
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
	p.Usage = strings.TrimSpace(p.Usage)
	p.Location = strings.TrimSpace(p.Location)
	if p.Operation == operatorSessionOperationReset {
		if p.Mode != operatorCacheModeCache && p.Mode != operatorCacheModeRefresh {
			return nil, fmt.Errorf("invalid mode %q", p.Mode)
		}
	}
	if (p.Operation == operatorSessionOperationRegister ||
		p.Operation == operatorSessionOperationEnterLocation ||
		p.Operation == operatorSessionOperationCompleteTarget ||
		p.Operation == operatorSessionOperationCompleteRestore ||
		p.Operation == operatorSessionOperationSkipRestore ||
		p.Operation == operatorSessionOperationExcludeSelected) &&
		p.Location == "" {
		return nil, fmt.Errorf("location is empty")
	}
	if p.Operation == operatorSessionOperationExcludeSelected &&
		p.Usage != operatorActionUsageTarget && p.Usage != operatorActionUsageRestore {
		return nil, fmt.Errorf("invalid usage %q", p.Usage)
	}
	return &p, nil
}

func operatorSessionReset(mode string) {
	uid := currentSellProductCacheUID()
	outpostProsperityMaxLocations := cachedOutpostProsperityMaxLocations(uid)
	operatorStateMu.Lock()
	defer operatorStateMu.Unlock()
	operatorSession = operatorSessionState{
		UID:                           uid,
		Mode:                          mode,
		ActiveLocations:               map[string]struct{}{},
		CompletedRestoreLocations:     map[string]struct{}{},
		EnteredLocations:              map[string]struct{}{},
		TargetAssignments:             map[string]operatorCandidate{},
		PlannedRestoreAssignments:     map[string]operatorCandidate{},
		LockedRestoreAssignments:      map[string]operatorCandidate{},
		ExcludedOperators:             map[string]struct{}{},
		OutpostProsperityMaxLocations: outpostProsperityMaxLocations,
		RetriedSelections:             map[string]struct{}{},
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
		UID:                           operatorSession.UID,
		Mode:                          operatorSession.Mode,
		ActiveLocations:               cloneStringSet(operatorSession.ActiveLocations),
		CompletedRestoreLocations:     cloneStringSet(operatorSession.CompletedRestoreLocations),
		EnteredLocations:              cloneStringSet(operatorSession.EnteredLocations),
		TargetAssignments:             cloneRestoreAssignments(operatorSession.TargetAssignments),
		PlannedRestoreAssignments:     cloneRestoreAssignments(operatorSession.PlannedRestoreAssignments),
		LockedRestoreAssignments:      cloneRestoreAssignments(operatorSession.LockedRestoreAssignments),
		ExcludedOperators:             cloneStringSet(operatorSession.ExcludedOperators),
		OutpostProsperityMaxLocations: cloneStringSet(operatorSession.OutpostProsperityMaxLocations),
		RetriedSelections:             cloneStringSet(operatorSession.RetriedSelections),
		CacheNoticePrinted:            operatorSession.CacheNoticePrinted,
		Refreshed:                     operatorSession.Refreshed,
	}
}

func operatorSessionSetTargetAssignment(location string, candidate operatorCandidate) {
	operatorStateMu.Lock()
	defer operatorStateMu.Unlock()
	ensureOperatorSessionLocked()
	operatorSession.TargetAssignments[location] = candidate
}

func operatorSessionTargetAssignment(location string) (operatorCandidate, bool) {
	operatorStateMu.Lock()
	defer operatorStateMu.Unlock()
	ensureOperatorSessionLocked()
	candidate, ok := operatorSession.TargetAssignments[location]
	return candidate, ok
}

func operatorSessionEnterLocation(location string) bool {
	operatorStateMu.Lock()
	defer operatorStateMu.Unlock()
	ensureOperatorSessionLocked()
	if _, entered := operatorSession.EnteredLocations[location]; entered {
		return false
	}
	operatorSession.EnteredLocations[location] = struct{}{}
	return true
}

func operatorSessionSetOutpostProsperityMax(location string, reached bool) string {
	operatorStateMu.Lock()
	defer operatorStateMu.Unlock()
	ensureOperatorSessionLocked()
	if reached {
		operatorSession.OutpostProsperityMaxLocations[location] = struct{}{}
		return operatorSession.UID
	}
	delete(operatorSession.OutpostProsperityMaxLocations, location)
	return operatorSession.UID
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

// operatorSessionExcludeSelected 将触发“已派驻”弹窗的候选加入本次任务排除集合。
// target 候选来自刚刚记录的售卖分配，restore 候选来自当前全局恢复方案。
func operatorSessionExcludeSelected(usage string, location string) (operatorCandidate, bool) {
	operatorStateMu.Lock()
	defer operatorStateMu.Unlock()
	ensureOperatorSessionLocked()

	var candidate operatorCandidate
	var ok bool
	switch usage {
	case operatorActionUsageTarget:
		candidate, ok = operatorSession.TargetAssignments[location]
		delete(operatorSession.TargetAssignments, location)
	case operatorActionUsageRestore:
		candidate, ok = operatorSession.PlannedRestoreAssignments[location]
		delete(operatorSession.PlannedRestoreAssignments, location)
	default:
		return operatorCandidate{}, false
	}
	if !ok {
		return operatorCandidate{}, false
	}
	operatorSession.ExcludedOperators[candidate.Name] = struct{}{}
	return candidate, true
}

func operatorSessionCompleteRestore(location string) (operatorCandidate, bool) {
	operatorStateMu.Lock()
	defer operatorStateMu.Unlock()
	ensureOperatorSessionLocked()
	candidate, ok := operatorSession.PlannedRestoreAssignments[location]
	if !ok {
		return operatorCandidate{}, false
	}
	operatorSession.LockedRestoreAssignments[location] = candidate
	operatorSession.CompletedRestoreLocations[location] = struct{}{}
	delete(operatorSession.TargetAssignments, location)
	delete(operatorSession.PlannedRestoreAssignments, location)
	return candidate, true
}

// operatorSessionSkipRestore 记录当前据点已经确认没有可用恢复干员。
// 后续重新规划必须排除该据点，避免继续为它预留其他据点需要的共享干员。
func operatorSessionSkipRestore(location string) {
	operatorStateMu.Lock()
	defer operatorStateMu.Unlock()
	ensureOperatorSessionLocked()
	operatorSession.CompletedRestoreLocations[location] = struct{}{}
	delete(operatorSession.TargetAssignments, location)
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

// 保证缓存加载或扫描提示在一次任务中只输出一次，避免识别节点重试时重复刷屏。
func operatorSessionClaimCacheNotice() bool {
	operatorStateMu.Lock()
	defer operatorStateMu.Unlock()
	ensureOperatorSessionLocked()
	if operatorSession.CacheNoticePrinted {
		return false
	}
	operatorSession.CacheNoticePrinted = true
	return true
}

func operatorSessionRefreshed() bool {
	operatorStateMu.Lock()
	defer operatorStateMu.Unlock()
	ensureOperatorSessionLocked()
	return operatorSession.Refreshed
}

func ensureOperatorSessionLocked() {
	uid := currentSellProductCacheUID()
	if operatorSession.UID == uid && operatorSession.ActiveLocations != nil {
		if operatorSession.EnteredLocations == nil {
			operatorSession.EnteredLocations = map[string]struct{}{}
		}
		if operatorSession.ExcludedOperators == nil {
			operatorSession.ExcludedOperators = map[string]struct{}{}
		}
		if operatorSession.OutpostProsperityMaxLocations == nil {
			operatorSession.OutpostProsperityMaxLocations = map[string]struct{}{}
		}
		return
	}
	outpostProsperityMaxLocations := cachedOutpostProsperityMaxLocations(uid)
	operatorSession = operatorSessionState{
		UID:                           uid,
		Mode:                          operatorCacheModeCache,
		ActiveLocations:               map[string]struct{}{},
		CompletedRestoreLocations:     map[string]struct{}{},
		EnteredLocations:              map[string]struct{}{},
		TargetAssignments:             map[string]operatorCandidate{},
		PlannedRestoreAssignments:     map[string]operatorCandidate{},
		LockedRestoreAssignments:      map[string]operatorCandidate{},
		ExcludedOperators:             map[string]struct{}{},
		OutpostProsperityMaxLocations: outpostProsperityMaxLocations,
		RetriedSelections:             map[string]struct{}{},
	}
}

func cachedOutpostProsperityMaxLocations(uid string) map[string]struct{} {
	locations, err := loadOutpostProsperityMaxLocations(uid)
	if err != nil {
		log.Warn().Err(err).
			Str("component", operatorSessionActionName).
			Str("uid", uid).
			Msg("failed to load outpost prosperity cache")
		return map[string]struct{}{}
	}
	return locations
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
