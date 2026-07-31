package operator

import (
	"encoding/json"
	"fmt"
	"strings"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const (
	selectBestOperatorRecognitionName      = "SellProductSelectBestOperator"
	currentBestOperatorRecognitionName     = "SellProductCurrentBestOperator"
	currentOperatorUncachedRecognitionName = "SellProductCurrentOperatorUncached"
	operatorCacheReadyRecognitionName      = "SellProductOperatorCacheReady"
	operatorListBottomRecognitionName      = "SellProductOperatorListBottom"
	operatorScanOutcomeRecognitionName     = "SellProductOperatorScanOutcome"
)

// operatorRecognitionParam 是 Pipeline 传入自定义识别器的原始参数。
// ROI 坐标以项目统一的 1280x720 基准分辨率表示。
type operatorRecognitionParam struct {
	Mode     string `json:"mode"`
	Usage    string `json:"usage"`
	Location string `json:"location"`
	Result   string `json:"result"`
	ROI      []int  `json:"roi"`
}

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

var _ maa.CustomRecognitionRunner = (*SelectBestOperatorRecognition)(nil)
var _ maa.CustomRecognitionRunner = (*CurrentBestOperatorRecognition)(nil)
var _ maa.CustomRecognitionRunner = (*CurrentOperatorUncachedRecognition)(nil)
var _ maa.CustomRecognitionRunner = (*OperatorCacheReadyRecognition)(nil)
var _ maa.CustomRecognitionRunner = (*OperatorListBottomRecognition)(nil)
var _ maa.CustomRecognitionRunner = (*OperatorScanOutcomeRecognition)(nil)

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
	p, err := parseOperatorRecognitionParam(arg.CustomRecognitionParam)
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
		Box:    match.Box,
		Detail: fmt.Sprintf("%s:%s", match.OCRText, candidate.Name),
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
	p, err := parseOperatorRecognitionParam(arg.CustomRecognitionParam)
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
	if selectionParam.Usage == operatorUsageTarget {
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
		Box:    match.Box,
		Detail: fmt.Sprintf("%s:%s", match.OCRText, candidate.Name),
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
	p, err := parseOperatorRecognitionParam(arg.CustomRecognitionParam)
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
		Box:    match.Box,
		Detail: fmt.Sprintf("%s:%s", match.OCRText, candidate.Name),
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
	p, err := parseOperatorRecognitionParam(arg.CustomRecognitionParam)
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
	p, err := parseOperatorRecognitionParam(arg.CustomRecognitionParam)
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
	p, err := parseOperatorRecognitionParam(arg.CustomRecognitionParam)
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
		if p.Usage == operatorUsageTarget {
			printRuntimeOperatorUnavailable(ctx, p.Location, p.Usage)
		}
	default:
		return nil, false
	}
	operatorListStateDelete(state.Key)
	return &maa.CustomRecognitionResult{Detail: operatorScanOutcomeDetailJSON(p, state)}, true
}

// parseOperatorRecognitionParam 解析并校验 Pipeline 传入的识别参数。
func parseOperatorRecognitionParam(raw string) (*operatorRecognitionParam, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("custom_recognition_param is empty")
	}

	var p operatorRecognitionParam
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		return nil, fmt.Errorf("unmarshal custom_recognition_param: %w", err)
	}
	p.Mode = strings.TrimSpace(p.Mode)
	if p.Mode != operatorCacheModeCache && p.Mode != operatorCacheModeRefresh {
		return nil, fmt.Errorf("invalid mode %q", p.Mode)
	}
	p.Usage = strings.TrimSpace(p.Usage)
	if p.Usage != operatorUsageTarget && p.Usage != operatorUsageRestore && p.Usage != operatorUsageAll {
		return nil, fmt.Errorf("invalid usage %q", p.Usage)
	}
	p.Location = strings.TrimSpace(p.Location)
	if p.Location == "" {
		return nil, fmt.Errorf("location is empty")
	}
	p.Result = strings.TrimSpace(p.Result)
	if len(p.ROI) != 4 {
		return nil, fmt.Errorf("invalid roi length %d, expected 4", len(p.ROI))
	}
	return &p, nil
}
