package goods

import (
	"testing"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/sellproduct/internal/selectiondata"
)

func testSelectionData() *selectiondata.File {
	return &selectiondata.File{
		Items: map[string]selectiondata.Item{
			"item_a": {Names: map[string]string{"zh_cn": "物品甲", "en_us": "Item A"}},
			"item_b": {Names: map[string]string{"zh_cn": "物品乙", "en_us": "Item B"}},
		},
		LocationOrder: []string{"TestOutpost"},
		Locations: map[string]selectiondata.Location{
			"TestOutpost": {
				Items: []selectiondata.LocationItem{
					{ItemID: "item_a", Rarity: 2, UnitPrice: 10},
					{ItemID: "item_b", Rarity: 3, UnitPrice: 20},
				},
			},
		},
	}
}

// TestBuildItemPriorityGroupsLoadsLocationItemAttributes 验证 Go 加载据点货品事实而不依赖预排序。
func TestBuildItemPriorityGroupsLoadsLocationItemAttributes(t *testing.T) {
	groupsByLocation, err := buildItemPriorityGroups(testSelectionData())
	if err != nil {
		t.Fatalf("buildItemPriorityGroups: %v", err)
	}
	groups := groupsByLocation["TestOutpost"]
	if len(groups) != 2 || groups[0].ItemID != "item_a" || groups[0].Rarity != 2 || groups[0].UnitPrice != 10 ||
		groups[1].ItemID != "item_b" || groups[1].Rarity != 3 || groups[1].UnitPrice != 20 {
		t.Fatalf("据点货品属性加载错误：%+v", groups)
	}
}

func TestBuildItemPriorityGroupsRejectsUnknownItem(t *testing.T) {
	data := testSelectionData()
	location := data.Locations["TestOutpost"]
	location.Items = append(location.Items, selectiondata.LocationItem{ItemID: "missing", Rarity: 1, UnitPrice: 1})
	data.Locations["TestOutpost"] = location
	if _, err := buildItemPriorityGroups(data); err == nil {
		t.Fatal("unknown item reference should fail")
	}
}

func TestBuildItemPriorityGroupsRejectsInvalidValueAttributes(t *testing.T) {
	for _, invalid := range []selectiondata.LocationItem{
		{ItemID: "item_a", UnitPrice: 10},
		{ItemID: "item_a", Rarity: 2},
	} {
		data := testSelectionData()
		location := data.Locations["TestOutpost"]
		location.Items[0] = invalid
		data.Locations["TestOutpost"] = location
		if _, err := buildItemPriorityGroups(data); err == nil {
			t.Fatalf("invalid location item %+v should fail", invalid)
		}
	}
}
