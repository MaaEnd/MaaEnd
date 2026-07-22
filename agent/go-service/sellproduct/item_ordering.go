package sellproduct

import (
	"fmt"
)

// itemPriorityGroup 是一个据点内的可售物品及其生成后自动售卖顺序。
type itemPriorityGroup struct {
	ItemID      string
	DisplayName string
	Candidates  []string
}

func buildItemPriorityGroups(data *sellProductSelectionDataFile) (map[string][]itemPriorityGroup, error) {
	if err := validateSellProductSelectionData(data); err != nil {
		return nil, err
	}
	result := make(map[string][]itemPriorityGroup, len(data.LocationOrder))
	for _, locationName := range data.LocationOrder {
		location, ok := data.Locations[locationName]
		if !ok {
			return nil, fmt.Errorf("location %q not found", locationName)
		}
		groups := make([]itemPriorityGroup, 0, len(location.ItemOrder))
		for _, itemID := range location.ItemOrder {
			group, err := selectionItemPriorityGroup(data, itemID)
			if err != nil {
				return nil, fmt.Errorf("location %q item order: %w", locationName, err)
			}
			groups = append(groups, group)
		}
		result[locationName] = groups
	}
	return result, nil
}

// prioritizeItemGroups 将用户指定物品按槽位顺序移到现有动态优先级之前。
// 不在当前据点的物品会被跳过；onlyPreferred 为 true 时只保留明确配置的物品，
// 否则其余物品继续保持原有稀有度、单价排序。
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

// findPriorityItemMatch 先还原尚未提交的 pending 货品，再跳过据点内已尝试、
// 任务内缺货和用户黑名单物品，按 groups 顺序选择最高优先级命中。
// recognized 记录本帧稳定识别到的所有已知货品，供耗尽判定使用。
func findPriorityItemMatch(
	ocrItems []ocrItem,
	groups []itemPriorityGroup,
	attempted map[string]struct{},
	outOfStock map[string]struct{},
	blacklisted map[string]struct{},
	pending string,
) (*matchResult, string, []string) {
	matches := make(map[string]*matchResult, len(groups))
	recognized := make([]string, 0, len(groups))
	for _, group := range groups {
		match := findBestMatch(ocrItems, group.Candidates)
		if match == nil {
			continue
		}
		matches[group.ItemID] = match
		recognized = append(recognized, group.ItemID)
	}
	if pending != "" {
		if match := matches[pending]; match != nil {
			return match, pending, recognized
		}
		// pending 只会在确认成功后提交。若本帧暂时未识别到它，不得改选
		// 更低优先级货品，否则一次 OCR 抖动就会改变已点击的选择。
		return nil, "", recognized
	}
	for _, group := range groups {
		if _, done := attempted[group.ItemID]; done {
			continue
		}
		if _, unavailable := outOfStock[group.ItemID]; unavailable {
			continue
		}
		if _, excluded := blacklisted[group.ItemID]; excluded {
			continue
		}
		if match := matches[group.ItemID]; match != nil {
			return match, group.ItemID, recognized
		}
	}
	return nil, "", recognized
}
