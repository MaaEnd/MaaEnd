package sellproduct

import (
	"encoding/json"
	"fmt"
	"image"
	"sort"
	"strings"
	"sync"
	"time"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

// SelectBestOperatorRecognition 在当前可见列表中寻找计划指定的全局最优干员。
// 命中框交给 Pipeline 点击；若当前页没有目标，则由 Pipeline 继续滚动列表。
type SelectBestOperatorRecognition struct{}

// CurrentBestOperatorRecognition 检查当前据点的最高加成档候选是否已经处于选中位置。
// 最高加成档优先取同时满足售卖和恢复的完美候选；同档沿用可减少无意义更换。
type CurrentBestOperatorRecognition struct{}

// CurrentOperatorUncachedRecognition 检查当前派驻干员是否是已知但未进入缓存快照的干员。
// 当前派驻干员一定为账号拥有；命中说明快照已过期，需重新完整扫描干员列表。
type CurrentOperatorUncachedRecognition struct{}

// OperatorCacheReadyRecognition 判断当前账号是否已有可用于选择的拥有干员快照。
type OperatorCacheReadyRecognition struct{}

// OperatorListBottomRecognition 累积列表扫描结果，并检测滚动是否已经到达底部。
type OperatorListBottomRecognition struct{}

// OperatorScanOutcomeRecognition 只读取已经完成的扫描结论，不重复处理当前截图。
// 它与 retry 节点放在同一个 next 列表中，避免同一心跳重复推进到底判定状态。
type OperatorScanOutcomeRecognition struct{}

type currentOperatorOCRCacheKey struct {
	taskID   int64
	location string
	roi      [4]int
}

type currentOperatorOCRCacheEntry struct {
	key   currentOperatorOCRCacheKey
	items []ocrItem
}

var _ maa.CustomRecognitionRunner = (*SelectBestOperatorRecognition)(nil)
var _ maa.CustomRecognitionRunner = (*CurrentBestOperatorRecognition)(nil)
var _ maa.CustomRecognitionRunner = (*CurrentOperatorUncachedRecognition)(nil)
var _ maa.CustomRecognitionRunner = (*OperatorCacheReadyRecognition)(nil)
var _ maa.CustomRecognitionRunner = (*OperatorListBottomRecognition)(nil)
var _ maa.CustomRecognitionRunner = (*OperatorScanOutcomeRecognition)(nil)

var currentOperatorOCRCache = struct {
	sync.Mutex
	entry *currentOperatorOCRCacheEntry
}{}

// Run 从当前画面的 OCR 结果中返回第一个可见的最优候选。
// 完整选择顺序由 candidatesForCurrentSelection 预先确定，因此这里无需再次计算权重。
func (r *SelectBestOperatorRecognition) Run(
	ctx *maa.Context,
	arg *maa.CustomRecognitionArg,
) (*maa.CustomRecognitionResult, bool) {
	if arg == nil {
		log.Error().Str("component", selectBestOperatorRecognitionName).Msg("got nil custom recognition arg")
		return nil, false
	}
	p, err := parseOperatorActionParam(arg.CustomRecognitionParam)
	if err != nil {
		log.Error().Err(err).Str("component", selectBestOperatorRecognitionName).Msg("invalid params")
		return nil, false
	}
	selectionParam, err := resolveOperatorSelectionParam(p)
	if err != nil {
		log.Error().Err(err).Str("component", selectBestOperatorRecognitionName).Msg("operator data unavailable")
		return nil, false
	}
	ownership, err := loadOperatorOwnershipForSelection()
	if err != nil {
		log.Error().Err(err).Str("component", selectBestOperatorRecognitionName).Msg("owned operators unavailable")
		return nil, false
	}
	candidates := candidatesForOwnership(selectionParam, ownership)
	if len(candidates) == 0 {
		return nil, false
	}
	setPlannedRestoreCandidate(selectionParam, candidates)

	items, err := recognizeOperatorList(ctx, arg.Img, p.ROI)
	if err != nil {
		log.Error().Err(err).Str("component", selectBestOperatorRecognitionName).Msg("recognize operator list failed")
		return nil, false
	}
	candidate, match, ok := findBestVisibleOperator(candidates, items)
	if !ok {
		return nil, false
	}
	recordTargetAssignment(p, candidate)
	operatorListStateDelete(operatorListScanStateKey(p))
	return &maa.CustomRecognitionResult{
		Box:    match.box,
		Detail: fmt.Sprintf("%s:%s", match.ocrText, candidate.Name),
	}, true
}

// Run 检查当前派驻是否属于当前据点的最高加成档，恢复阶段仍只检查全局规划候选。
func (r *CurrentBestOperatorRecognition) Run(
	ctx *maa.Context,
	arg *maa.CustomRecognitionArg,
) (*maa.CustomRecognitionResult, bool) {
	if arg == nil {
		log.Error().Str("component", currentBestOperatorRecognitionName).Msg("got nil custom recognition arg")
		return nil, false
	}
	p, err := parseOperatorActionParam(arg.CustomRecognitionParam)
	if err != nil {
		log.Error().Err(err).Str("component", currentBestOperatorRecognitionName).Msg("invalid params")
		return nil, false
	}
	selectionParam, err := resolveOperatorSelectionParam(p)
	if err != nil {
		log.Error().Err(err).Str("component", currentBestOperatorRecognitionName).Msg("operator data unavailable")
		return nil, false
	}
	ownership, err := loadOperatorOwnershipForSelection()
	if err != nil {
		log.Error().Err(err).Str("component", currentBestOperatorRecognitionName).Msg("owned operators unavailable")
		return nil, false
	}
	var candidates []operatorCandidate
	if selectionParam.Usage == operatorActionUsageTarget {
		candidates = equivalentTargetCandidatesForOwnership(selectionParam, ownership)
	} else {
		candidates = candidatesForOwnership(selectionParam, ownership)
	}
	if len(candidates) == 0 {
		return nil, false
	}
	setPlannedRestoreCandidate(selectionParam, candidates)

	items, err := recognizeCurrentOperatorList(ctx, arg, p, true)
	if err != nil {
		log.Error().Err(err).Str("component", currentBestOperatorRecognitionName).Msg("recognize current operator failed")
		return nil, false
	}
	candidate, match, ok := findCurrentBestOperator(candidates, selectionParam.KnownOperators, items)
	if !ok {
		return nil, false
	}
	recordTargetAssignment(p, candidate)
	operatorListStateDelete(operatorListScanStateKey(p))
	return &maa.CustomRecognitionResult{
		Box:    match.box,
		Detail: fmt.Sprintf("%s:%s", match.ocrText, candidate.Name),
	}, true
}

// Run 识别当前派驻干员；若其为已知干员却不在缓存快照中，则使快照失效并命中。
// 是否重新扫描以及扫描后的流程走向由 Pipeline 决定；一次任务最多触发一次，
// 且本任务已完成过完整扫描时不再重复触发，避免扫描本身遗漏干员时无限重扫。
func (r *CurrentOperatorUncachedRecognition) Run(
	ctx *maa.Context,
	arg *maa.CustomRecognitionArg,
) (*maa.CustomRecognitionResult, bool) {
	if arg == nil {
		log.Error().Str("component", currentOperatorUncachedRecognitionName).Msg("got nil custom recognition arg")
		return nil, false
	}
	p, err := parseOperatorActionParam(arg.CustomRecognitionParam)
	if err != nil {
		log.Error().Err(err).Str("component", currentOperatorUncachedRecognitionName).Msg("invalid params")
		return nil, false
	}
	data, err := loadOperatorSelectionDataFunc()
	if err != nil {
		log.Error().Err(err).Str("component", currentOperatorUncachedRecognitionName).Msg("operator data unavailable")
		return nil, false
	}
	if data == nil || len(data.KnownOperators) == 0 {
		log.Error().Str("component", currentOperatorUncachedRecognitionName).Msg("known operator data is empty")
		return nil, false
	}
	ownership, err := loadOperatorOwnershipForSelection()
	if err != nil {
		log.Error().Err(err).Str("component", currentOperatorUncachedRecognitionName).Msg("owned operators unavailable")
		return nil, false
	}
	items, err := recognizeCurrentOperatorList(ctx, arg, p, false)
	if err != nil {
		log.Error().Err(err).Str("component", currentOperatorUncachedRecognitionName).Msg("recognize current operator failed")
		return nil, false
	}
	candidate, match, ok := findUncachedCurrentOperator(data.KnownOperators, ownership, items)
	if !ok {
		return nil, false
	}
	if !operatorSessionClaimCacheRescan() {
		log.Debug().Str("component", currentOperatorUncachedRecognitionName).
			Str("operator", candidate.Name).
			Str("location", p.Location).
			Msg("cache rescan already triggered in this task")
		return nil, false
	}
	if operatorSessionRefreshed() {
		log.Warn().Str("component", currentOperatorUncachedRecognitionName).
			Str("operator", candidate.Name).
			Str("location", p.Location).
			Msg("current operator still missing after this task's full scan, rescan skipped")
		return nil, false
	}
	if err := invalidateOperatorSnapshotForUID(resolveSellProductCachePathFunc(), currentSellProductCacheUID()); err != nil {
		log.Error().Err(err).Str("component", currentOperatorUncachedRecognitionName).
			Str("operator", candidate.Name).
			Msg("invalidate operator cache failed")
		return nil, false
	}
	log.Warn().Str("component", currentOperatorUncachedRecognitionName).
		Str("operator", candidate.Name).
		Str("location", p.Location).
		Msg("current operator missing from cache, operator cache invalidated")
	printRuntimeOperatorCacheRescan(ctx, candidate)
	return &maa.CustomRecognitionResult{
		Box:    match.box,
		Detail: fmt.Sprintf("%s:%s", match.ocrText, candidate.Name),
	}, true
}

// Run 将缓存是否可用转换为 Pipeline 可识别的布尔命中结果。
func (r *OperatorCacheReadyRecognition) Run(
	ctx *maa.Context,
	arg *maa.CustomRecognitionArg,
) (*maa.CustomRecognitionResult, bool) {
	if arg == nil {
		log.Error().Str("component", operatorCacheReadyRecognitionName).Msg("got nil custom recognition arg")
		return nil, false
	}
	p, err := parseOperatorActionParam(arg.CustomRecognitionParam)
	if err != nil {
		log.Error().Err(err).Str("component", operatorCacheReadyRecognitionName).Msg("invalid params")
		return nil, false
	}
	status, err := operatorCacheStatusForSelection(p)
	if err != nil {
		log.Error().Err(err).Str("component", operatorCacheReadyRecognitionName).Msg("read operator cache failed")
		return nil, false
	}
	if operatorSessionClaimCacheNotice() {
		printRuntimeOperatorCacheStatus(ctx, status)
	}
	if status.Ready {
		return &maa.CustomRecognitionResult{Detail: "cache_ready"}, true
	}
	return nil, false
}

// Run 维护一次跨多帧、跨多次滚动的列表扫描状态。
// 每帧都会累积识别到的相关干员；当连续两帧 OCR 签名相同，视为滚动已无法推进。
// 只有全局首次扫描或用户主动刷新时才写入完整快照；据点内找人只复用既有缓存重新规划。
func (r *OperatorListBottomRecognition) Run(
	ctx *maa.Context,
	arg *maa.CustomRecognitionArg,
) (*maa.CustomRecognitionResult, bool) {
	if arg == nil {
		log.Error().Str("component", operatorListBottomRecognitionName).Msg("got nil custom recognition arg")
		return nil, false
	}
	p, err := parseOperatorActionParam(arg.CustomRecognitionParam)
	if err != nil {
		log.Error().Err(err).Str("component", operatorListBottomRecognitionName).Msg("invalid params")
		return nil, false
	}
	state := operatorListStateFor(p)
	if state.Completed {
		return operatorListBottomResult(p, state)
	}
	selectionParam, err := resolveOperatorSelectionParam(p)
	if err != nil {
		log.Error().Err(err).Str("component", operatorListBottomRecognitionName).Msg("operator data unavailable")
		state.Completed = true
		state.Error = err.Error()
		operatorListStateSet(state)
		return nil, false
	}
	scanCandidates := collectScanCandidates(selectionParam)
	items, err := recognizeOperatorList(ctx, arg.Img, p.ROI)
	if err != nil {
		log.Error().Err(err).Str("component", operatorListBottomRecognitionName).Msg("recognize operator list failed")
		state.Completed = true
		state.Error = err.Error()
		operatorListStateSet(state)
		return nil, false
	}
	observed := observedOperatorIDs(items, scanCandidates)
	state.Observed = append(state.Observed, observed...)
	signature := operatorListSignature(observed)
	// Pipeline 在每次识别失败后继续向下滚动；相邻两帧内容一致说明已经到达底部。
	reachedBottom := operatorListReachedBottom(state.PreviousSignature, signature)
	if !reachedBottom {
		state.PreviousSignature = signature
		operatorListStateSet(state)
		return nil, false
	}
	if err := replaceObservedOperators(p, scanCandidates, state.Observed); err != nil {
		log.Error().Err(err).Str("component", operatorListBottomRecognitionName).Msg("cache refresh failed")
		state.Completed = true
		state.Error = err.Error()
		operatorListStateSet(state)
		return nil, false
	}
	ownership, err := loadOperatorOwnershipForSelection()
	if err != nil {
		log.Error().Err(err).Str("component", operatorListBottomRecognitionName).Msg("reload refreshed cache failed")
		state.Completed = true
		state.Error = err.Error()
		operatorListStateSet(state)
		return nil, false
	}
	candidates := candidatesForOwnership(selectionParam, ownership)
	setPlannedRestoreCandidate(selectionParam, candidates)
	configuredCandidates := configuredCandidatesForOutcome(selectionParam)
	state.ExpectedCandidates = operatorCandidateIDs(configuredCandidates)
	state.ObservedCandidates = observedConfiguredOperatorNames(configuredCandidates, state.Observed)
	state.Completed = true
	state.HasCandidate = len(candidates) > 0
	if p.Result == operatorListBottomResultRetry && state.HasCandidate &&
		!operatorSessionClaimRetry(p.Usage, p.Location) {
		state.Error = "operator still unavailable after refreshed retry"
		state.HasCandidate = false
	}
	if p.Result == operatorListBottomResultRetry && state.HasCandidate {
		printRuntimeOperatorReplanned(ctx, p.Location, p.Usage, candidates[0])
	}
	operatorListStateSet(state)
	return operatorListBottomResult(p, state)
}

func (r *OperatorScanOutcomeRecognition) Run(
	ctx *maa.Context,
	arg *maa.CustomRecognitionArg,
) (*maa.CustomRecognitionResult, bool) {
	if arg == nil {
		log.Error().Str("component", operatorScanOutcomeRecognitionName).Msg("got nil custom recognition arg")
		return nil, false
	}
	p, err := parseOperatorActionParam(arg.CustomRecognitionParam)
	if err != nil {
		log.Error().Err(err).Str("component", operatorScanOutcomeRecognitionName).Msg("invalid params")
		return nil, false
	}
	state, ok := operatorListStateGet(operatorListScanStateKey(p))
	if !ok || !state.Completed {
		return nil, false
	}
	switch p.Result {
	case operatorListBottomResultError:
		if state.Error == "" {
			return nil, false
		}
		printRuntimeOperatorScanFailed(ctx, p.Location, p.Usage)
	case operatorListBottomResultNotFound:
		if state.Error != "" || state.HasCandidate {
			return nil, false
		}
		if p.Usage == operatorActionUsageTarget {
			printRuntimeOperatorUnavailable(ctx, p.Location, p.Usage)
		}
	default:
		return nil, false
	}
	operatorListStateDelete(state.Key)
	return &maa.CustomRecognitionResult{Detail: operatorScanOutcomeDetailJSON(p, state)}, true
}

// resolveOperatorSelectionParam 将轻量 Pipeline 参数与资源派生数据合并为运行时选择参数。
func resolveOperatorSelectionParam(p *operatorActionParam) (*operatorSelectionParam, error) {
	data, err := loadOperatorSelectionDataFunc()
	if err != nil {
		return nil, err
	}
	if data == nil {
		return nil, fmt.Errorf("operator selection data is nil")
	}
	if p.Usage != operatorActionUsageAll {
		if _, ok := data.TargetCandidates[p.Location]; !ok {
			return nil, fmt.Errorf("operator data not found for location %q", p.Location)
		}
	}
	scanCandidates := allOperatorScanCandidates(data)
	if len(scanCandidates) == 0 {
		return nil, fmt.Errorf("known operator data is empty")
	}
	session := operatorSessionSnapshot()
	if len(session.ActiveLocations) == 0 {
		return nil, fmt.Errorf("active locations are empty")
	}
	if p.Usage != operatorActionUsageAll {
		if _, active := session.ActiveLocations[p.Location]; !active {
			return nil, fmt.Errorf("location %q is not active", p.Location)
		}
	}
	result := &operatorSelectionParam{
		Usage:                         p.Usage,
		Location:                      p.Location,
		TargetCandidatesByLocation:    data.TargetCandidates,
		RestoreGroups:                 normalizeOperatorCandidateGroups(data.RestoreGroups),
		ScanCandidates:                scanCandidates,
		KnownOperators:                data.KnownOperators,
		ActiveLocations:               session.ActiveLocations,
		CompletedRestoreLocations:     session.CompletedRestoreLocations,
		TargetAssignments:             session.TargetAssignments,
		LockedRestoreAssignments:      session.LockedRestoreAssignments,
		ExcludedOperators:             session.ExcludedOperators,
		OutpostProsperityMaxLocations: session.OutpostProsperityMaxLocations,
	}
	switch p.Usage {
	case operatorActionUsageTarget:
		result.Candidates = normalizeOperatorCandidates(data.TargetCandidates[p.Location])
	case operatorActionUsageRestore, operatorActionUsageAll:
	default:
		return nil, fmt.Errorf("invalid usage %q", p.Usage)
	}
	return result, nil
}

// allOperatorScanCandidates 返回配置中的完整干员表，形成缓存刷新识别域。
func allOperatorScanCandidates(data *operatorSelectionData) []operatorCandidate {
	if data == nil {
		return nil
	}
	return normalizeOperatorCandidates(data.KnownOperators)
}

func setPlannedRestoreCandidate(p *operatorSelectionParam, candidates []operatorCandidate) {
	if p.Usage != operatorActionUsageRestore {
		return
	}
	if len(candidates) == 0 {
		operatorSessionSetPlannedRestore(p.Location, operatorCandidate{}, false)
		return
	}
	operatorSessionSetPlannedRestore(p.Location, candidates[0], true)
}

func recordTargetAssignment(p *operatorActionParam, candidate operatorCandidate) {
	if p.Usage == operatorActionUsageTarget {
		operatorSessionSetTargetAssignment(p.Location, candidate)
	}
}

// operatorListScanState 保存一次列表滚动扫描的跨帧状态。
// PreviousSignature 用于到底判定；Observed 允许在多个页面累计拥有干员；
// Completed 和 HasCandidate 供同一心跳里的只读分支节点消费。
type operatorListScanState struct {
	Key                string
	PreviousSignature  string
	Observed           []string
	ExpectedCandidates []string
	ObservedCandidates []string
	Completed          bool
	HasCandidate       bool
	Error              string
}

type operatorScanOutcomeDetail struct {
	Result             string   `json:"result"`
	Reason             string   `json:"reason"`
	Usage              string   `json:"usage"`
	Location           string   `json:"location"`
	ExpectedCandidates []string `json:"expected_candidates,omitempty"`
	ObservedCandidates []string `json:"observed_candidates,omitempty"`
	Error              string   `json:"error,omitempty"`
}

// operatorListScanStates 仅保存当前进程内的短期扫描状态，键中包含 UID 和选择参数。
var operatorListScanStates = map[string]operatorListScanState{}

// loadOperatorOwnershipForSelection 读取当前账号完整快照中的拥有干员集合。
func loadOperatorOwnershipForSelection() (operatorOwnership, error) {
	uid := currentSellProductCacheUID()
	path := resolveSellProductCachePathFunc()
	cache, err := readSellProductCache(path)
	if err != nil {
		return operatorOwnership{}, err
	}
	return operatorOwnership{
		Operators: operatorIDSet(cachedOperatorIDsForUID(cache, uid)),
	}, nil
}

type operatorCacheStatus struct {
	Ready     bool
	UpdatedAt time.Time
}

// operatorCacheStatusForSelection 返回当前缓存是否可消费，以及实际复用缓存的更新时间。
// cache 模式仅复用完整快照，没有快照时先扫描全部干员；refresh 模式始终等待本次任务完成扫描。
func operatorCacheStatusForSelection(p *operatorActionParam) (operatorCacheStatus, error) {
	if p.Mode == operatorCacheModeRefresh {
		return operatorCacheStatus{Ready: operatorSessionRefreshed()}, nil
	}
	uid := currentSellProductCacheUID()
	path := resolveSellProductCachePathFunc()
	cache, err := readSellProductCache(path)
	if err != nil {
		return operatorCacheStatus{}, err
	}
	if !sellProductCacheHasOperatorSnapshot(cache, uid) {
		return operatorCacheStatus{}, nil
	}
	return operatorCacheStatus{
		Ready:     true,
		UpdatedAt: cachedOperatorUpdatedAtForUID(cache, uid),
	}, nil
}

// replaceObservedOperators 仅在全局首次扫描或主动刷新时写入当前账号的完整快照。
func replaceObservedOperators(
	p *operatorActionParam,
	scanCandidates []operatorCandidate,
	observed []string,
) error {
	if p == nil {
		return fmt.Errorf("operator action param is nil")
	}
	uid := currentSellProductCacheUID()
	path := resolveSellProductCachePathFunc()
	sellProductCacheMu.Lock()
	defer sellProductCacheMu.Unlock()
	cache, err := readSellProductCache(path)
	if err != nil {
		return err
	}
	if !shouldWriteOperatorCacheSnapshot(p, cache, uid) {
		log.Debug().
			Str("component", operatorListBottomRecognitionName).
			Str("mode", p.Mode).
			Str("usage", p.Usage).
			Str("location", p.Location).
			Msg("operator cache write skipped")
		return nil
	}
	cache = mergeOperatorSnapshot(cache, uid, scanCandidates, observed, time.Now())
	if err := writeSellProductCache(path, cache); err != nil {
		return err
	}
	operatorSessionMarkRefreshed()
	return nil
}

// shouldWriteOperatorCacheSnapshot 限制缓存只能由全局完整扫描创建或主动刷新。
func shouldWriteOperatorCacheSnapshot(
	p *operatorActionParam,
	cache sellProductCache,
	uid string,
) bool {
	if p == nil || p.Usage != operatorActionUsageAll || p.Location != "global" {
		return false
	}
	if p.Mode == operatorCacheModeRefresh {
		return true
	}
	return p.Mode == operatorCacheModeCache && !sellProductCacheHasOperatorSnapshot(cache, uid)
}

// observedOperatorIDs 将一帧 OCR 结果映射成去重、排序后的干员 ID 集合。
func observedOperatorIDs(items []ocrItem, candidates []operatorCandidate) []string {
	observedSet := map[string]struct{}{}
	for _, candidate := range candidates {
		if findBestMatch(items, candidate.Expected) != nil {
			observedSet[candidate.Name] = struct{}{}
		}
	}
	return sortedSetValues(observedSet)
}

// operatorListSignature 使用当前画面识别到的规范化干员名称生成稳定签名。
// 非干员 OCR 文本不参与签名，避免头像和界面噪声波动干扰到底判定。
func operatorListSignature(operatorNames []string) string {
	if len(operatorNames) == 0 {
		return ""
	}
	normalizedNames := uniqueNonEmptyStrings(operatorNames)
	sort.Strings(normalizedNames)
	return strings.Join(normalizedNames, "\n")
}

// operatorListReachedBottom 通过连续两帧非空签名相同判断列表已经无法继续滚动。
// 空签名不参与判断，避免 OCR 暂时失败时把空页面误认为列表底部。
func operatorListReachedBottom(previousSignature string, currentSignature string) bool {
	return previousSignature != "" && previousSignature == currentSignature
}

// findBestVisibleOperator 只匹配计划指定的全局最优候选。
// 即使次优候选在当前页可见，也必须继续滚动查找第一名，不能提前降级选择。
func findBestVisibleOperator(candidates []operatorCandidate, items []ocrItem) (operatorCandidate, *matchResult, bool) {
	if len(candidates) == 0 {
		return operatorCandidate{}, nil, false
	}
	candidate := candidates[0]
	match := findBestMatch(items, candidate.Expected)
	if match != nil {
		return candidate, match, true
	}
	return operatorCandidate{}, nil, false
}

// findCurrentBestOperator 按稳定顺序匹配当前据点最高加成档中的任一当前干员。
func findCurrentBestOperator(
	candidates []operatorCandidate,
	knownOperators []operatorCandidate,
	items []ocrItem,
) (operatorCandidate, *matchResult, bool) {
	if len(candidates) == 0 {
		return operatorCandidate{}, nil, false
	}
	for _, candidate := range candidates {
		match := findBestMatch(items, candidate.Expected)
		if match == nil {
			match = findCurrentOperatorPrefixMatch(items, candidate, knownOperators)
		}
		if match != nil {
			return candidate, match, true
		}
	}
	return operatorCandidate{}, nil, false
}

// findUncachedCurrentOperator 判断当前派驻干员是否为已知但未进入缓存快照的干员。
// 复用当前干员的精确与前缀噪声匹配；未识别出已知干员时不作结论，避免 OCR 噪声误判。
func findUncachedCurrentOperator(
	knownOperators []operatorCandidate,
	ownership operatorOwnership,
	items []ocrItem,
) (operatorCandidate, *matchResult, bool) {
	candidate, match, ok := findCurrentBestOperator(knownOperators, knownOperators, items)
	if !ok {
		return operatorCandidate{}, nil, false
	}
	if _, owned := ownership.Operators[candidate.Name]; owned {
		return operatorCandidate{}, nil, false
	}
	return candidate, match, true
}

// findCurrentOperatorPrefixMatch 处理当前干员名称与右侧界面文本被 OCR 合并的情况。
// 仅当目标名称是 OCR 文本前缀，且不存在更长的已知干员名称同样匹配该前缀时才命中。
func findCurrentOperatorPrefixMatch(
	items []ocrItem,
	target operatorCandidate,
	knownOperators []operatorCandidate,
) *matchResult {
	sortedItems := sortOCRItemsByPosition(items)
	for _, item := range sortedItems {
		ocrCore := stripSeparators(item.text)
		if ocrCore == "" {
			continue
		}
		for _, candidate := range target.Expected {
			candidateCore := stripSeparators(candidate)
			if candidateCore == "" || ocrCore == candidateCore || !strings.HasPrefix(ocrCore, candidateCore) {
				continue
			}
			if hasLongerKnownOperatorPrefix(ocrCore, candidateCore, target, knownOperators) {
				continue
			}
			return &matchResult{
				ocrText:   item.text,
				candidate: candidate,
				tier:      "operator_prefix_noise",
				box:       item.box,
			}
		}
	}
	return nil
}

// hasLongerKnownOperatorPrefix 判断 OCR 是否更可能是另一个名称更长的已知干员。
func hasLongerKnownOperatorPrefix(
	ocrCore string,
	targetCore string,
	target operatorCandidate,
	knownOperators []operatorCandidate,
) bool {
	targetLength := len([]rune(targetCore))
	for _, operator := range knownOperators {
		if sameOperator(operator, target) {
			continue
		}
		for _, expected := range operator.Expected {
			knownCore := stripSeparators(expected)
			if len([]rune(knownCore)) <= targetLength {
				continue
			}
			if strings.HasPrefix(ocrCore, knownCore) {
				return true
			}
		}
	}
	return false
}

// recognizeOperatorList 在指定 720p 基准 ROI 内运行 MaaFramework OCR，并转换为统一结果格式。
func recognizeOperatorList(ctx *maa.Context, img image.Image, roi []int) ([]ocrItem, error) {
	detail, err := ctx.RunRecognitionDirect(
		maa.RecognitionTypeOCR,
		maa.OCRParam{ROI: maa.NewTargetRect(maa.Rect{roi[0], roi[1], roi[2], roi[3]})},
		img,
	)
	if err != nil {
		return nil, err
	}
	return collectOCRResults(detail), nil
}

// recognizeCurrentOperatorList 让相邻的当前干员判断复用同一截图、同一 ROI 的 OCR 结果。
// uncached 节点先写入一次性缓存；紧随其后的 best 节点按任务、据点和 ROI 读取并消费。
func recognizeCurrentOperatorList(
	ctx *maa.Context,
	arg *maa.CustomRecognitionArg,
	p *operatorActionParam,
	reuse bool,
) ([]ocrItem, error) {
	key := makeCurrentOperatorOCRCacheKey(arg, p)
	if reuse {
		if items, ok := takeCurrentOperatorOCRCache(key); ok {
			return items, nil
		}
	}

	items, err := recognizeOperatorList(ctx, arg.Img, p.ROI)
	if err != nil {
		return nil, err
	}
	if !reuse {
		storeCurrentOperatorOCRCache(key, items)
	}
	return items, nil
}

func makeCurrentOperatorOCRCacheKey(
	arg *maa.CustomRecognitionArg,
	p *operatorActionParam,
) currentOperatorOCRCacheKey {
	return currentOperatorOCRCacheKey{
		taskID:   arg.TaskID,
		location: p.Location,
		roi:      [4]int{p.ROI[0], p.ROI[1], p.ROI[2], p.ROI[3]},
	}
}

func storeCurrentOperatorOCRCache(key currentOperatorOCRCacheKey, items []ocrItem) {
	currentOperatorOCRCache.Lock()
	defer currentOperatorOCRCache.Unlock()
	currentOperatorOCRCache.entry = &currentOperatorOCRCacheEntry{
		key:   key,
		items: append([]ocrItem(nil), items...),
	}
}

func takeCurrentOperatorOCRCache(key currentOperatorOCRCacheKey) ([]ocrItem, bool) {
	currentOperatorOCRCache.Lock()
	defer currentOperatorOCRCache.Unlock()
	entry := currentOperatorOCRCache.entry
	currentOperatorOCRCache.entry = nil
	if entry == nil || entry.key != key {
		return nil, false
	}
	return append([]ocrItem(nil), entry.items...), true
}

// operatorListStateFor 获取现有扫描状态，或根据磁盘缓存初始化新的扫描会话。
func operatorListStateFor(p *operatorActionParam) operatorListScanState {
	key := operatorListScanStateKey(p)
	if state, ok := operatorListStateGet(key); ok {
		return state
	}
	return operatorListScanState{
		Key: key,
	}
}

// shouldHitOperatorListBottomResult 根据完整扫描后的重新规划结果选择 Pipeline 分支。
func shouldHitOperatorListBottomResult(p *operatorActionParam, hasCandidate bool) bool {
	switch p.Result {
	case operatorListBottomResultScanDone:
		return true
	case operatorListBottomResultRetry:
		return hasCandidate
	case operatorListBottomResultNotFound:
		return !hasCandidate
	default:
		return true
	}
}

func operatorListBottomResult(
	p *operatorActionParam,
	state operatorListScanState,
) (*maa.CustomRecognitionResult, bool) {
	if state.Error != "" {
		return nil, false
	}
	if !shouldHitOperatorListBottomResult(p, state.HasCandidate) {
		return nil, false
	}
	operatorListStateDelete(state.Key)
	if p.Result == operatorListBottomResultNotFound {
		return &maa.CustomRecognitionResult{Detail: operatorScanOutcomeDetailJSON(p, state)}, true
	}
	return &maa.CustomRecognitionResult{Detail: p.Result}, true
}

// configuredCandidatesForOutcome 返回当前据点需要在失败详情中展示的候选干员。
func configuredCandidatesForOutcome(p *operatorSelectionParam) []operatorCandidate {
	if p == nil {
		return nil
	}
	if p.Usage == operatorActionUsageTarget {
		return p.Candidates
	}
	if p.Usage == operatorActionUsageRestore {
		for _, group := range p.RestoreGroups {
			if group.Location == p.Location {
				return group.Candidates
			}
		}
	}
	return nil
}

func operatorCandidateIDs(candidates []operatorCandidate) []string {
	names := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		names = append(names, candidate.Name)
	}
	return uniqueNonEmptyStrings(names)
}

// observedConfiguredOperatorNames 按候选优先级返回本次完整扫描实际观察到的候选。
func observedConfiguredOperatorNames(candidates []operatorCandidate, observed []string) []string {
	observedSet := operatorIDSet(observed)
	names := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		name := candidate.Name
		if _, ok := observedSet[name]; ok {
			names = append(names, name)
		}
	}
	return uniqueNonEmptyStrings(names)
}

// operatorScanOutcomeDetailJSON 序列化终止分支详情，便于日志直接指出失败据点和候选干员。
func operatorScanOutcomeDetailJSON(p *operatorActionParam, state operatorListScanState) string {
	reason := "scan_error"
	if state.Error == "" {
		switch p.Usage {
		case operatorActionUsageTarget:
			reason = "no_owned_candidate"
		case operatorActionUsageRestore:
			reason = "no_available_candidate"
		default:
			reason = "no_candidate"
		}
	}
	detail, err := json.Marshal(operatorScanOutcomeDetail{
		Result:             p.Result,
		Reason:             reason,
		Usage:              p.Usage,
		Location:           p.Location,
		ExpectedCandidates: state.ExpectedCandidates,
		ObservedCandidates: state.ObservedCandidates,
		Error:              state.Error,
	})
	if err != nil {
		return p.Result
	}
	return string(detail)
}

// operatorListScanStateKey 为一次具体的列表扫描生成进程内隔离键。
func operatorListScanStateKey(p *operatorActionParam) string {
	return strings.Join([]string{
		currentSellProductCacheUID(),
		p.Mode,
		p.Usage,
		p.Location,
	}, "|")
}
