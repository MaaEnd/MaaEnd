package strategy

import "sort"

// Price 按单价和稀有度选择货品。
type Price struct{}

var _ Orderer = Price{}

// Select 依次按单价、稀有度和稳定输入顺序返回最佳候选。
func (Price) Select(candidates []Candidate) (Candidate, bool) {
	if len(candidates) == 0 {
		return Candidate{}, false
	}
	bestIndex := 0
	for index := 1; index < len(candidates); index++ {
		if betterPriceCandidate(candidates[index], candidates[bestIndex]) {
			bestIndex = index
		}
	}
	return candidates[bestIndex], true
}

// Sort 返回按 Price 规则排序的候选副本。
func (Price) Sort(candidates []Candidate) []Candidate {
	result := append([]Candidate(nil), candidates...)
	sort.SliceStable(result, func(left, right int) bool {
		return betterPriceCandidate(result[left], result[right])
	})
	return result
}

func betterPriceCandidate(candidate Candidate, best Candidate) bool {
	if candidate.UnitPrice != best.UnitPrice {
		return candidate.UnitPrice > best.UnitPrice
	}
	return candidate.Rarity > best.Rarity
}
