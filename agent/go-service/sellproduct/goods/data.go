package goods

import (
	"fmt"
	"strings"
	"sync"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/sellproduct/internal/selectiondata"
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
		Candidates: candidates,
		Rarity:     locationItem.Rarity,
		UnitPrice:  locationItem.UnitPrice,
	}, nil
}
