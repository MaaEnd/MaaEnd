package strategy

import "sort"

// Rarity 按稀有度和单价选择货品。
type Rarity struct{}

var _ Orderer = Rarity{}

// Select 依次按稀有度、单价和稳定输入顺序返回最佳候选。
func (Rarity) Select(candidates []Candidate) (Candidate, bool) {
	if len(candidates) == 0 {
		return Candidate{}, false
	}
	bestIndex := 0
	for index := 1; index < len(candidates); index++ {
		if betterRarityCandidate(candidates[index], candidates[bestIndex]) {
			bestIndex = index
		}
	}
	return candidates[bestIndex], true
}

// Sort 返回按 Rarity 规则排序的候选副本。
func (Rarity) Sort(candidates []Candidate) []Candidate {
	result := append([]Candidate(nil), candidates...)
	sort.SliceStable(result, func(left, right int) bool {
		return betterRarityCandidate(result[left], result[right])
	})
	return result
}

func betterRarityCandidate(candidate Candidate, best Candidate) bool {
	if candidate.Rarity != best.Rarity {
		return candidate.Rarity > best.Rarity
	}
	return candidate.UnitPrice > best.UnitPrice
}
