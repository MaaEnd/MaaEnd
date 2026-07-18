package sellproduct

import (
	"fmt"
	"sync"
)

var (
	// loadItemSelectionGroupsFunc 是为单元测试保留的替换点。
	loadItemSelectionGroupsFunc = loadItemSelectionGroupsCached
	itemSelectionGroupsOnce     sync.Once
	itemSelectionGroupsCache    []itemSelectionGroup
	itemSelectionGroupsErr      error
	loadItemPriorityGroupsFunc  = loadItemPriorityGroupsCached
	itemPriorityGroupsOnce      sync.Once
	itemPriorityGroupsCache     map[string][]itemPriorityGroup
	itemPriorityGroupsErr       error
)

// itemSelectionGroup 保存一个货品的稳定 ID 和全部语言名称。
// Pipeline 只负责提供截图 ROI，默认选品识别则使用这里的数据判断实际点中了哪个货品。
type itemSelectionGroup struct {
	ItemID      string
	DisplayName string
	Candidates  []string
}

func loadItemSelectionGroups() ([]itemSelectionGroup, error) {
	data, err := loadSellProductSelectionDataCached()
	if err != nil {
		return nil, err
	}
	return buildItemSelectionGroups(data)
}

// loadItemSelectionGroupsCached 在 Agent 生命周期内复用不可变的货品名称数据。
func loadItemSelectionGroupsCached() ([]itemSelectionGroup, error) {
	itemSelectionGroupsOnce.Do(func() {
		itemSelectionGroupsCache, itemSelectionGroupsErr = loadItemSelectionGroups()
	})
	return itemSelectionGroupsCache, itemSelectionGroupsErr
}

func loadItemPriorityGroups() (map[string][]itemPriorityGroup, error) {
	data, err := loadSellProductSelectionDataCached()
	if err != nil {
		return nil, err
	}
	return buildItemPriorityGroups(data)
}

func loadItemPriorityGroupsCached() (map[string][]itemPriorityGroup, error) {
	itemPriorityGroupsOnce.Do(func() {
		itemPriorityGroupsCache, itemPriorityGroupsErr = loadItemPriorityGroups()
	})
	return itemPriorityGroupsCache, itemPriorityGroupsErr
}

func buildItemSelectionGroups(data *sellProductSelectionDataFile) ([]itemSelectionGroup, error) {
	if err := validateSellProductSelectionData(data); err != nil {
		return nil, err
	}
	groups := make([]itemSelectionGroup, 0, len(data.ItemOrder))
	for _, itemID := range data.ItemOrder {
		group, err := selectionItemGroup(data, itemID)
		if err != nil {
			return nil, fmt.Errorf("item order: %w", err)
		}
		groups = append(groups, group)
	}
	return groups, nil
}

// findBestItemMatch 复用货品名称的严格抗噪声匹配，并把命中的名称还原成 itemId。
func findBestItemMatch(ocrItems []ocrItem, groups []itemSelectionGroup) (*matchResult, string) {
	var candidates []string
	itemIDByCandidate := make(map[string]string)
	for _, group := range groups {
		for _, candidate := range group.Candidates {
			candidates = append(candidates, candidate)
			if _, exists := itemIDByCandidate[candidate]; !exists {
				itemIDByCandidate[candidate] = group.ItemID
			}
		}
	}
	match := findBestMatch(ocrItems, candidates)
	if match == nil {
		return nil, ""
	}
	return match, itemIDByCandidate[match.candidate]
}
