package strategy

// Stock 依次按库存、单价、稀有度和稳定输入顺序选择货品。
type Stock struct {
	MinimumUnitPrice int
}

var _ Selector = Stock{}

// Select 排除低于最低单价的候选并返回最佳货品。
func (s Stock) Select(candidates []Candidate) (Candidate, bool) {
	bestIndex := -1
	for index, candidate := range candidates {
		if !candidate.StockKnown || candidate.UnitPrice < s.MinimumUnitPrice {
			continue
		}
		if bestIndex < 0 || betterStockCandidate(candidate, candidates[bestIndex]) {
			bestIndex = index
		}
	}
	if bestIndex < 0 {
		return Candidate{}, false
	}
	return candidates[bestIndex], true
}

func betterStockCandidate(candidate Candidate, best Candidate) bool {
	if candidate.Stock != best.Stock {
		return candidate.Stock > best.Stock
	}
	if candidate.UnitPrice != best.UnitPrice {
		return candidate.UnitPrice > best.UnitPrice
	}
	return candidate.Rarity > best.Rarity
}
