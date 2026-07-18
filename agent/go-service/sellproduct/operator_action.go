package sellproduct

import (
	"encoding/json"
	"fmt"
	"image"
	"sort"
	"strings"
	"time"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

// SelectBestOperatorRecognition 在当前可见列表中寻找计划指定的全局最优干员。
// 命中框交给 Pipeline 点击；若当前页没有目标，则由 Pipeline 继续滚动列表。
type SelectBestOperatorRecognition struct{}

// CurrentBestOperatorRecognition 只检查全局最优候选当前是否已经处于选中位置。
// 它用于避免重复点击已选中的干员，而不是在当前页面降级选择次优候选。
type CurrentBestOperatorRecognition struct{}

// OperatorCacheReadyRecognition 判断当前账号是否已有可用于选择的拥有干员快照。
type OperatorCacheReadyRecognition struct{}

// OperatorListBottomRecognition 累积列表扫描结果，并检测滚动是否已经到达底部。
type OperatorListBottomRecognition struct{}

// OperatorScanOutcomeRecognition 只读取已经完成的扫描结论，不重复处理当前截图。
// 它与 retry 节点放在同一个 next 列表中，避免同一心跳重复推进到底判定状态。
type OperatorScanOutcomeRecognition struct{}

var _ maa.CustomRecognitionRunner = (*SelectBestOperatorRecognition)(nil)
var _ maa.CustomRecognitionRunner = (*CurrentBestOperatorRecognition)(nil)
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
	if _, err := recordObservedOperators([]string{operatorCandidateCacheName(candidate)}); err != nil {
		// 缓存是优化手段，不能让一次有效的界面识别因磁盘写入失败而失效。
		log.Warn().Err(err).Str("component", selectBestOperatorRecognitionName).Msg("cache update failed")
	}
	recordTargetAssignment(p, candidate)
	operatorListStateDelete(operatorListScanStateKey(p))
	return &maa.CustomRecognitionResult{
		Box:    match.box,
		Detail: fmt.Sprintf("%s:%s", match.ocrText, candidate.Name),
	}, true
}

// Run 检查排序第一的全局最优候选是否出现在当前画面。
// 与 SelectBestOperatorRecognition 的区别是：本方法不会在最优候选不可见时返回次优项。
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
	candidates := candidatesForOwnership(selectionParam, ownership)
	if len(candidates) == 0 {
		return nil, false
	}
	setPlannedRestoreCandidate(selectionParam, candidates)

	items, err := recognizeOperatorList(ctx, arg.Img, p.ROI)
	if err != nil {
		log.Error().Err(err).Str("component", currentBestOperatorRecognitionName).Msg("recognize current operator failed")
		return nil, false
	}
	candidate, match, ok := findCurrentBestOperator(candidates, selectionParam.KnownOperators, items)
	if !ok {
		return nil, false
	}
	if _, err := recordObservedOperators([]string{operatorCandidateCacheName(candidate)}); err != nil {
		log.Warn().Err(err).Str("component", currentBestOperatorRecognitionName).Msg("cache update failed")
	}
	recordTargetAssignment(p, candidate)
	operatorListStateDelete(operatorListScanStateKey(p))
	return &maa.CustomRecognitionResult{
		Box:    match.box,
		Detail: fmt.Sprintf("%s:%s", match.ocrText, candidate.Name),
	}, true
}

// Run 将缓存是否可用转换为 Pipeline 可识别的布尔命中结果。
func (r *OperatorCacheReadyRecognition) Run(
	_ *maa.Context,
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
	if operatorCacheReadyForSelection(p) {
		return &maa.CustomRecognitionResult{Detail: "cache_ready"}, true
	}
	return nil, false
}

// Run 维护一次跨多帧、跨多次滚动的列表扫描状态。
// 每帧都会累积识别到的相关干员；当连续两帧 OCR 签名相同，视为滚动已无法推进，
// 此时写入完整快照并根据新的拥有集合重新规划。
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
	observed := observedOperatorCacheNames(items, scanCandidates)
	state.Observed = append(state.Observed, observed...)
	signature := operatorListSignature(items)
	// Pipeline 在每次识别失败后继续向下滚动；相邻两帧内容一致说明已经到达底部。
	reachedBottom := operatorListReachedBottom(state.PreviousSignature, signature)
	if !reachedBottom {
		state.PreviousSignature = signature
		operatorListStateSet(state)
		return nil, false
	}
	// 搜索从列表顶部开始并到达底部，因此本轮观察结果可以升级为权威快照。
	if err := replaceObservedOperators(scanCandidates, state.Observed); err != nil {
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
	state.ExpectedCandidates = operatorCandidateCacheNames(configuredCandidates)
	state.ObservedCandidates = observedConfiguredOperatorNames(configuredCandidates, state.Observed)
	state.Completed = true
	state.HasCandidate = len(candidates) > 0
	if p.Result == operatorListBottomResultRetry && state.HasCandidate &&
		!operatorSessionClaimRetry(p.Usage, p.Location) {
		state.Error = "operator still unavailable after refreshed retry"
		state.HasCandidate = false
	}
	operatorListStateSet(state)
	return operatorListBottomResult(p, state)
}

func (r *OperatorScanOutcomeRecognition) Run(
	_ *maa.Context,
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
	case operatorListBottomResultNotFound:
		if state.Error != "" || state.HasCandidate {
			return nil, false
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
	session := operatorSessionSnapshot()
	result := &operatorSelectionParam{
		Usage:                      p.Usage,
		Location:                   p.Location,
		TargetCandidatesByLocation: data.TargetCandidates,
		ScanCandidates:             allOperatorScanCandidates(data),
		KnownOperators:             data.KnownOperators,
		ActiveLocations:            session.ActiveLocations,
		CompletedRestoreLocations:  session.CompletedRestoreLocations,
		TargetAssignments:          session.TargetAssignments,
		LockedRestoreAssignments:   session.LockedRestoreAssignments,
		ExcludedOperators:          session.ExcludedOperators,
	}
	switch p.Usage {
	case operatorActionUsageTarget:
		result.Candidates = normalizeOperatorCandidates(data.TargetCandidates[p.Location])
	case operatorActionUsageRestore:
		result.RestoreGroups = normalizeOperatorCandidateGroups(data.RestoreGroups)
	case operatorActionUsageAll:
	default:
		return nil, fmt.Errorf("invalid usage %q", p.Usage)
	}
	if len(result.KnownOperators) == 0 {
		result.KnownOperators = result.ScanCandidates
	}
	return result, nil
}

// allOperatorScanCandidates 合并全部据点的售卖与恢复候选，形成缓存刷新识别域。
func allOperatorScanCandidates(data *operatorSelectionData) []operatorCandidate {
	if data == nil {
		return nil
	}
	var candidates []operatorCandidate
	for _, targetCandidates := range data.TargetCandidates {
		candidates = append(candidates, targetCandidates...)
	}
	for _, group := range data.RestoreGroups {
		candidates = append(candidates, group.Candidates...)
	}
	return normalizeOperatorCandidates(candidates)
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

// loadOperatorOwnershipForSelection 读取当前账号的拥有干员集合及其完整性。
// 不完整缓存只表示部分干员已确认拥有，不能据此排除尚未观察到的候选。
func loadOperatorOwnershipForSelection() (operatorOwnership, error) {
	uid := currentOperatorCacheUID()
	path := resolveOperatorCachePathFunc(uid)
	cache, err := readOperatorCache(path)
	if err != nil {
		return operatorOwnership{}, err
	}
	return operatorOwnership{
		Operators: operatorNameSet(operatorCacheOperatorsForUID(cache, uid)),
		Complete:  operatorCacheHasSnapshot(cache, uid),
	}, nil
}

// operatorCacheReadyForSelection 判断当前模式下缓存是否已经达到可消费状态。
// cache 模式允许直接进入渐进搜索；refresh 模式必须等待本次任务完成一次全列表扫描。
func operatorCacheReadyForSelection(p *operatorActionParam) bool {
	if p.Mode == operatorCacheModeRefresh {
		return operatorSessionRefreshed()
	}
	return true
}

// recordObservedOperators 把局部 OCR 确认拥有的干员合并进当前账号缓存，并返回最新集合。
func recordObservedOperators(observed []string) (map[string]struct{}, error) {
	uid := currentOperatorCacheUID()
	path := resolveOperatorCachePathFunc(uid)
	cache, err := readOperatorCache(path)
	if err != nil {
		return nil, err
	}
	if len(observed) > 0 {
		cache = mergeObservedOperatorCache(cache, uid, observed, time.Now())
		if err := writeOperatorCacheFile(path, cache); err != nil {
			return nil, err
		}
	}
	return operatorNameSet(operatorCacheOperatorsForUID(cache, uid)), nil
}

// replaceObservedOperators 用完整扫描结果刷新候选域，并在成功落盘后标记本次会话已刷新。
func replaceObservedOperators(scanCandidates []operatorCandidate, observed []string) error {
	uid := currentOperatorCacheUID()
	path := resolveOperatorCachePathFunc(uid)
	cache, err := readOperatorCache(path)
	if err != nil {
		return err
	}
	cache = mergeOperatorCache(cache, uid, scanCandidates, observed, time.Now())
	if err := writeOperatorCacheFile(path, cache); err != nil {
		return err
	}
	operatorSessionMarkRefreshed()
	return nil
}

// observedOperatorCacheNames 将一帧 OCR 结果映射成去重、排序后的缓存键集合。
func observedOperatorCacheNames(items []ocrItem, candidates []operatorCandidate) []string {
	observedSet := map[string]struct{}{}
	for _, candidate := range candidates {
		if findBestMatch(items, candidate.Expected) != nil {
			observedSet[operatorCandidateCacheName(candidate)] = struct{}{}
		}
	}
	return sortedSetValues(observedSet)
}

// operatorListSignature 为当前 OCR 列表生成与引擎返回顺序无关的稳定签名。
// 先按坐标和文本排序，再只拼接文本；这样 OCR 并发返回顺序变化不会误判列表仍在滚动。
func operatorListSignature(items []ocrItem) string {
	if len(items) == 0 {
		return ""
	}
	sortedItems := make([]ocrItem, 0, len(items))
	for _, item := range items {
		text := strings.TrimSpace(item.text)
		if text == "" {
			continue
		}
		item.text = text
		sortedItems = append(sortedItems, item)
	}
	sort.SliceStable(sortedItems, func(i, j int) bool {
		if sortedItems[i].box.Y() != sortedItems[j].box.Y() {
			return sortedItems[i].box.Y() < sortedItems[j].box.Y()
		}
		if sortedItems[i].box.X() != sortedItems[j].box.X() {
			return sortedItems[i].box.X() < sortedItems[j].box.X()
		}
		return sortedItems[i].text < sortedItems[j].text
	})

	var b strings.Builder
	for _, item := range sortedItems {
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(item.text)
	}
	return b.String()
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

// findCurrentBestOperator 只匹配候选列表中的全局第一名，用于判断是否已经选中最优干员。
func findCurrentBestOperator(
	candidates []operatorCandidate,
	knownOperators []operatorCandidate,
	items []ocrItem,
) (operatorCandidate, *matchResult, bool) {
	if len(candidates) == 0 {
		return operatorCandidate{}, nil, false
	}
	candidate := candidates[0]
	match := findBestMatch(items, candidate.Expected)
	if match == nil {
		match = findCurrentOperatorPrefixMatch(items, candidate, knownOperators)
	}
	if match == nil {
		return operatorCandidate{}, nil, false
	}
	return candidate, match, true
}

// findCurrentOperatorPrefixMatch 兼容当前干员名称与右侧界面文本被 OCR 合并的情况。
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

func operatorCandidateCacheNames(candidates []operatorCandidate) []string {
	names := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		names = append(names, operatorCandidateCacheName(candidate))
	}
	return uniqueNonEmptyStrings(names)
}

// observedConfiguredOperatorNames 按候选优先级返回本次完整扫描实际观察到的候选。
func observedConfiguredOperatorNames(candidates []operatorCandidate, observed []string) []string {
	observedSet := operatorNameSet(observed)
	names := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		name := operatorCandidateCacheName(candidate)
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
		currentOperatorCacheUID(),
		p.Mode,
		p.Usage,
		p.Location,
	}, "|")
}
