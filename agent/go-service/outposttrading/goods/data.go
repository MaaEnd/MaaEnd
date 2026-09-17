package goods

import (
	"fmt"
	"strings"
	"sync"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/outposttrading/internal/selectiondata"
)

var (
	loadItemPriorityGroupsFunc = loadItemPriorityGroupsCached
	itemPriorityGroupsOnce     sync.Once
	itemPriorityGroupsCache    map[string][]itemPriorityGroup
	itemPriorityGroupsErr      error
)

// itemPriorityGroup 是一个据点内的可售物品及其价值属性。
type itemPriorityGroup struct {
	ItemID     string
	ActivityID string
	Candidates []string
	Rarity     int
	UnitPrice  int
}

func loadItemPriorityGroups() (map[string][]itemPriorityGroup, error) {
	data, err := selectiondata.LoadCached()
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

// itemUnitPrice 在指定据点的货品列表中按物品 ID 查询基础单价和活动 ID。
// 未配置 activity_id 的物品返回空字符串。
func itemUnitPrice(location, itemID string) (int, string, error) {
	if location == "" {
		return 0, "", fmt.Errorf("location is empty")
	}
	if itemID == "" {
		return 0, "", fmt.Errorf("item ID is empty")
	}
	groups, err := loadItemPriorityGroupsFunc()
	if err != nil {
		return 0, "", err
	}
	locationGroups, ok := groups[location]
	if !ok {
		return 0, "", fmt.Errorf("location %q not found", location)
	}
	for _, group := range locationGroups {
		if group.ItemID == itemID {
			if group.UnitPrice <= 0 {
				return 0, "", fmt.Errorf("invalid unit price for item %q at %q", itemID, location)
			}
			return group.UnitPrice, group.ActivityID, nil
		}
	}
	return 0, "", fmt.Errorf("item %q not found at location %q", itemID, location)
}

func buildItemPriorityGroups(data *selectiondata.File) (map[string][]itemPriorityGroup, error) {
	if err := selectiondata.ValidateGoods(data); err != nil {
		return nil, err
	}
	result := make(map[string][]itemPriorityGroup, len(data.LocationOrder))
	for _, locationName := range data.LocationOrder {
		location, ok := data.Locations[locationName]
		if !ok {
			return nil, fmt.Errorf("location %q not found", locationName)
		}
		groups := make([]itemPriorityGroup, 0, len(location.Items))
		for _, locationItem := range location.Items {
			group, err := itemPriorityGroupFromData(data, locationItem)
			if err != nil {
				return nil, fmt.Errorf("location %q item: %w", locationName, err)
			}
			groups = append(groups, group)
		}
		result[locationName] = groups
	}
	return result, nil
}

func itemPriorityGroupFromData(
	data *selectiondata.File,
	locationItem selectiondata.LocationItem,
) (itemPriorityGroup, error) {
	itemID := strings.TrimSpace(locationItem.ItemID)
	item, ok := data.Items[itemID]
	if !ok {
		return itemPriorityGroup{}, fmt.Errorf("item %q not found", itemID)
	}
	if locationItem.Rarity <= 0 {
		return itemPriorityGroup{}, fmt.Errorf("item %q rarity must be positive", itemID)
	}
	if locationItem.UnitPrice <= 0 {
		return itemPriorityGroup{}, fmt.Errorf("item %q unit price must be positive", itemID)
	}
	candidates := selectiondata.ExpectedNames(item.Names)
	if len(candidates) == 0 {
		return itemPriorityGroup{}, fmt.Errorf("item %q expected names are empty", itemID)
	}
	return itemPriorityGroup{
		ItemID:     itemID,
		ActivityID: strings.TrimSpace(locationItem.ActivityID),
		Candidates: candidates,
		Rarity:     locationItem.Rarity,
		UnitPrice:  locationItem.UnitPrice,
	}, nil
}
