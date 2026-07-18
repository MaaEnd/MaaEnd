package sellproduct

import "sync"

var (
	loadItemPriorityGroupsFunc = loadItemPriorityGroupsCached
	itemPriorityGroupsOnce     sync.Once
	itemPriorityGroupsCache    map[string][]itemPriorityGroup
	itemPriorityGroupsErr      error
)

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
