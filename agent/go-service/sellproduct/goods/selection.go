package goods

import (
	"sort"

	sellstrategy "github.com/MaaXYZ/MaaEnd/agent/go-service/sellproduct/goods/strategy"
)

// prioritizeItemGroups 将用户指定物品按槽位顺序移到据点货品之前。
// 不在当前据点的物品会被跳过；onlyPreferred 为 true 时只保留明确配置的物品，
// 否则其余物品继续保持稳定来源顺序。
func prioritizeItemGroups(groups []itemPriorityGroup, preferred []string, onlyPreferred bool) []itemPriorityGroup {
	result := make([]itemPriorityGroup, 0, len(groups))
	groupsByID := make(map[string]itemPriorityGroup, len(groups))
	for _, group := range groups {
		groupsByID[group.ItemID] = group
	}
	added := make(map[string]struct{}, len(groups))
	for _, itemID := range preferred {
		if _, exists := added[itemID]; exists {
			continue
		}
		group, exists := groupsByID[itemID]
		if !exists {
			continue
		}
		result = append(result, group)
		added[itemID] = struct{}{}
	}
	if onlyPreferred {
		return result
	}
	for _, group := range groups {
		if _, exists := added[group.ItemID]; exists {
			continue
		}
		result = append(result, group)
		added[group.ItemID] = struct{}{}
	}
	return result
}

func selectGoodsTarget(
	location string,
	groups []itemPriorityGroup,
	policy prioritySelectionPolicy,
	observations map[string]sellstrategy.Candidate,
) (string, []string) {
	attempted, outOfStock, pending := prioritySelectionSnapshot(location)
	blacklisted := reserveBlacklistedItemsSnapshot()
	reserveSatisfied := reserveSatisfiedItemsSnapshot()
	recognized := make([]string, 0, len(observations))
	for itemID := range observations {
		recognized = append(recognized, itemID)
	}
	sort.Strings(recognized)
	selector, ok := sellstrategy.New(policy.Strategy, policy.StrategyConfig)
	if !ok {
		return "", recognized
	}
	if pending != "" {
		if observation, visible := observations[pending]; visible && stockCandidateAvailable(observation) {
			if _, selectable := selectGoodsCandidate(
				selector,
				[]sellstrategy.Candidate{observation},
				policy.Preferred,
			); selectable {
				return pending, recognized
			}
		}
		return "", recognized
	}

	candidates := make([]sellstrategy.Candidate, 0, len(groups))
	for _, group := range groups {
		observation, ok := observations[group.ItemID]
		if !ok || !stockCandidateAvailable(observation) {
			continue
		}
		if _, done := attempted[group.ItemID]; done {
			continue
		}
		if _, unavailable := outOfStock[group.ItemID]; unavailable {
			continue
		}
		if _, excluded := blacklisted[group.ItemID]; excluded {
			continue
		}
		if _, satisfied := reserveSatisfied[group.ItemID]; satisfied {
			continue
		}
		candidates = append(candidates, observation)
	}

	selected, ok := selectGoodsCandidate(selector, candidates, policy.Preferred)
	if !ok {
		return "", recognized
	}
	return selected.ItemID, recognized
}

func stockCandidateAvailable(candidate sellstrategy.Candidate) bool {
	return !candidate.StockKnown || candidate.Stock > 0
}

// selectGoodsCandidate 先按用户配置顺序选择可用优先货品，不应用选品策略；
// 没有可用优先货品时再由策略从全部公共候选中选择。
func selectGoodsCandidate(
	selector sellstrategy.Selector,
	candidates []sellstrategy.Candidate,
	preferred []string,
) (sellstrategy.Candidate, bool) {
	candidatesByID := make(map[string]sellstrategy.Candidate, len(candidates))
	for _, candidate := range candidates {
		candidatesByID[candidate.ItemID] = candidate
	}
	for _, itemID := range preferred {
		candidate, ok := candidatesByID[itemID]
		if !ok {
			continue
		}
		return candidate, true
	}
	return selector.Select(candidates)
}

func buildStockObservations(
	items []stockPageItem,
	groups []itemPriorityGroup,
) map[string]sellstrategy.Candidate {
	groupsByID := make(map[string]itemPriorityGroup, len(groups))
	for _, group := range groups {
		groupsByID[group.ItemID] = group
	}
	observations := make(map[string]sellstrategy.Candidate, len(items))
	for _, item := range items {
		group := groupsByID[item.ItemID]
		observations[item.ItemID] = sellstrategy.Candidate{
			ItemID:     item.ItemID,
			Stock:      item.Quantity,
			StockKnown: item.StockKnown,
			Rarity:     group.Rarity,
			UnitPrice:  group.UnitPrice,
		}
	}
	return observations
}

func findStockPageItem(items []stockPageItem, itemID string) (stockPageItem, bool) {
	for _, item := range items {
		if item.ItemID == itemID {
			return item, true
		}
	}
	return stockPageItem{}, false
}
