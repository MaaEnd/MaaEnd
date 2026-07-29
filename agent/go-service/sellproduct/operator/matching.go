package operator

import (
	"strings"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/sellproduct/internal/ocrmatch"
)

// findBestVisibleOperator 只匹配计划指定的全局最优候选。
// 即使次优候选在当前页可见，也必须继续滚动查找第一名，不能提前降级选择。
func findBestVisibleOperator(
	candidates []operatorCandidate,
	items []ocrmatch.Item,
) (operatorCandidate, *ocrmatch.Result, bool) {
	if len(candidates) == 0 {
		return operatorCandidate{}, nil, false
	}
	candidate := candidates[0]
	match := ocrmatch.FindBest(items, candidate.Expected)
	if match != nil {
		return candidate, match, true
	}
	return operatorCandidate{}, nil, false
}

// findCurrentBestOperator 按稳定顺序匹配当前据点最高加成档中的任一当前干员。
func findCurrentBestOperator(
	candidates []operatorCandidate,
	knownOperators []operatorCandidate,
	items []ocrmatch.Item,
) (operatorCandidate, *ocrmatch.Result, bool) {
	if len(candidates) == 0 {
		return operatorCandidate{}, nil, false
	}
	for _, candidate := range candidates {
		match := ocrmatch.FindBest(items, candidate.Expected)
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
	items []ocrmatch.Item,
) (operatorCandidate, *ocrmatch.Result, bool) {
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
	items []ocrmatch.Item,
	target operatorCandidate,
	knownOperators []operatorCandidate,
) *ocrmatch.Result {
	sortedItems := ocrmatch.SortItemsByPosition(items)
	for _, item := range sortedItems {
		ocrCore := ocrmatch.StripSeparators(item.Text)
		if ocrCore == "" {
			continue
		}
		for _, candidate := range target.Expected {
			candidateCore := ocrmatch.StripSeparators(candidate)
			if candidateCore == "" || ocrCore == candidateCore || !strings.HasPrefix(ocrCore, candidateCore) {
				continue
			}
			if hasLongerKnownOperatorPrefix(ocrCore, candidateCore, target, knownOperators) {
				continue
			}
			return &ocrmatch.Result{
				OCRText:   item.Text,
				Candidate: candidate,
				Tier:      "operator_prefix_noise",
				Box:       item.Box,
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
			knownCore := ocrmatch.StripSeparators(expected)
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
