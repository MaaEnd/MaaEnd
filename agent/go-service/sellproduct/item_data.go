package sellproduct

import (
	"sort"
	"strings"
	"sync"
)

var (
	// loadItemSelectionGroupsFunc 是为单元测试保留的替换点。
	loadItemSelectionGroupsFunc = loadItemSelectionGroupsCached
	itemSelectionGroupsOnce     sync.Once
	itemSelectionGroupsCache    []itemSelectionGroup
	itemSelectionGroupsErr      error
)

// itemSelectionGroup 保存一个货品的稳定 ID 和全部语言名称。
// Pipeline 只负责提供截图 ROI，默认选品识别则使用这里的数据判断实际点中了哪个货品。
type itemSelectionGroup struct {
	ItemID     string
	Candidates []string
}

func loadItemSelectionGroups() ([]itemSelectionGroup, error) {
	var data settlementTradeFile
	if err := readSettlementTradeFile(&data); err != nil {
		return nil, err
	}
	return buildItemSelectionGroups(data), nil
}

// loadItemSelectionGroupsCached 在 Agent 生命周期内复用不可变的货品名称数据。
func loadItemSelectionGroupsCached() ([]itemSelectionGroup, error) {
	itemSelectionGroupsOnce.Do(func() {
		itemSelectionGroupsCache, itemSelectionGroupsErr = loadItemSelectionGroups()
	})
	return itemSelectionGroupsCache, itemSelectionGroupsErr
}

// buildItemSelectionGroups 按据点、繁荣度和资源原始顺序收集货品。
// 同一 itemId 可能出现在多个繁荣度中，因此会合并其多语言名称并只输出一次。
func buildItemSelectionGroups(data settlementTradeFile) []itemSelectionGroup {
	groups := make([]itemSelectionGroup, 0)
	indexByItemID := make(map[string]int)
	for _, location := range settlementLocations(data) {
		levels := make([]string, 0, len(location.Settlement.ByProsperityLevel))
		for level := range location.Settlement.ByProsperityLevel {
			levels = append(levels, level)
		}
		sort.Strings(levels)
		for _, level := range levels {
			for _, item := range location.Settlement.ByProsperityLevel[level].TradeItems {
				itemID := strings.TrimSpace(item.ItemID)
				if itemID == "" {
					continue
				}
				candidates := uniqueNonEmptyStrings([]string{
					item.Name["CN"],
					item.Name["TC"],
					item.Name["EN"],
					item.Name["JP"],
					item.Name["KR"],
				})
				if len(candidates) == 0 {
					continue
				}
				if index, ok := indexByItemID[itemID]; ok {
					groups[index].Candidates = uniqueNonEmptyStrings(append(groups[index].Candidates, candidates...))
					continue
				}
				indexByItemID[itemID] = len(groups)
				groups = append(groups, itemSelectionGroup{ItemID: itemID, Candidates: candidates})
			}
		}
	}
	return groups
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
