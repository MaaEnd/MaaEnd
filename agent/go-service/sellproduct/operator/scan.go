package operator

import (
	"encoding/json"
	"fmt"
	"image"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/sellproduct/internal/ocrmatch"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const (
	// cache 仅复用完整本地快照，缺失时先扫描；refresh 强制先完整扫描一次干员列表。
	operatorCacheModeCache   = "cache"
	operatorCacheModeRefresh = "refresh"

	// usage 决定本次选择针对目标据点、恢复原岗位，还是仅扫描全部候选干员。
	operatorUsageTarget  = "target"
	operatorUsageRestore = "restore"
	operatorUsageAll     = "all"

	// 列表到底后，Pipeline 根据 result 区分“扫描完成”和“缓存中未找到目标”两条分支。
	operatorListBottomResultScanDone = "scan_done"
	operatorListBottomResultRetry    = "retry"
	operatorListBottomResultNotFound = "not_found"
	operatorListBottomResultError    = "error"
)

type currentOperatorOCRCacheKey struct {
	taskID   int64
	location string
	roi      [4]int
}

type currentOperatorOCRCacheEntry struct {
	key   currentOperatorOCRCacheKey
	items []ocrmatch.Item
}

var currentOperatorOCRCache = struct {
	sync.Mutex
	entry *currentOperatorOCRCacheEntry
}{}

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
func operatorCacheStatusForSelection(p *operatorRecognitionParam) (operatorCacheStatus, error) {
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
	p *operatorRecognitionParam, scanCandidates []operatorCandidate,
	observed []string,
) error {
	if p == nil {
		return fmt.Errorf("operator recognition param is nil")
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
	p *operatorRecognitionParam, cache sellProductCache,
	uid string,
) bool {
	if p == nil || p.Usage != operatorUsageAll || p.Location != "global" {
		return false
	}
	if p.Mode == operatorCacheModeRefresh {
		return true
	}
	return p.Mode == operatorCacheModeCache && !sellProductCacheHasOperatorSnapshot(cache, uid)
}

// observedOperatorIDs 将一帧 OCR 结果映射成去重、排序后的干员 ID 集合。
func observedOperatorIDs(items []ocrmatch.Item, candidates []operatorCandidate) []string {
	observedSet := map[string]struct{}{}
	for _, candidate := range candidates {
		if ocrmatch.FindBest(items, candidate.Expected) != nil {
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

// recognizeOperatorList 在指定 720p 基准 ROI 内运行 MaaFramework OCR，并转换为统一结果格式。
func recognizeOperatorList(ctx *maa.Context, img image.Image, roi []int) ([]ocrmatch.Item, error) {
	detail, err := ctx.RunRecognitionDirect(
		maa.RecognitionTypeOCR,
		maa.OCRParam{ROI: maa.NewTargetRect(maa.Rect{roi[0], roi[1], roi[2], roi[3]})},
		img,
	)
	if err != nil {
		return nil, err
	}
	return ocrmatch.CollectResults(detail), nil
}

// recognizeCurrentOperatorList 让相邻的当前干员判断复用同一截图、同一 ROI 的 OCR 结果。
// uncached 节点先写入一次性缓存；紧随其后的 best 节点按任务、据点和 ROI 读取并消费。
func recognizeCurrentOperatorList(
	ctx *maa.Context,
	arg *maa.CustomRecognitionArg,
	p *operatorRecognitionParam, reuse bool,
) ([]ocrmatch.Item, error) {
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
	p *operatorRecognitionParam) currentOperatorOCRCacheKey {
	return currentOperatorOCRCacheKey{
		taskID:   arg.TaskID,
		location: p.Location,
		roi:      [4]int{p.ROI[0], p.ROI[1], p.ROI[2], p.ROI[3]},
	}
}

func storeCurrentOperatorOCRCache(key currentOperatorOCRCacheKey, items []ocrmatch.Item) {
	currentOperatorOCRCache.Lock()
	defer currentOperatorOCRCache.Unlock()
	currentOperatorOCRCache.entry = &currentOperatorOCRCacheEntry{
		key:   key,
		items: append([]ocrmatch.Item(nil), items...),
	}
}

func takeCurrentOperatorOCRCache(key currentOperatorOCRCacheKey) ([]ocrmatch.Item, bool) {
	currentOperatorOCRCache.Lock()
	defer currentOperatorOCRCache.Unlock()
	entry := currentOperatorOCRCache.entry
	currentOperatorOCRCache.entry = nil
	if entry == nil || entry.key != key {
		return nil, false
	}
	return append([]ocrmatch.Item(nil), entry.items...), true
}

// operatorListStateFor 获取现有扫描状态，或根据磁盘缓存初始化新的扫描会话。
func operatorListStateFor(p *operatorRecognitionParam) operatorListScanState {
	key := operatorListScanStateKey(p)
	if state, ok := operatorListStateGet(key); ok {
		return state
	}
	return operatorListScanState{
		Key: key,
	}
}

// shouldHitOperatorListBottomResult 根据完整扫描后的重新规划结果选择 Pipeline 分支。
func shouldHitOperatorListBottomResult(p *operatorRecognitionParam, hasCandidate bool) bool {
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
	p *operatorRecognitionParam, state operatorListScanState,
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
	if p.Usage == operatorUsageTarget {
		return p.Candidates
	}
	if p.Usage == operatorUsageRestore {
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
func operatorScanOutcomeDetailJSON(p *operatorRecognitionParam, state operatorListScanState) string {
	reason := "scan_error"
	if state.Error == "" {
		switch p.Usage {
		case operatorUsageTarget:
			reason = "no_owned_candidate"
		case operatorUsageRestore:
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
func operatorListScanStateKey(p *operatorRecognitionParam) string {
	return strings.Join([]string{
		currentSellProductCacheUID(),
		p.Mode,
		p.Usage,
		p.Location,
	}, "|")
}
