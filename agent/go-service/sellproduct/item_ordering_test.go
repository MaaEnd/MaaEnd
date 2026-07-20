package sellproduct

import (
	"reflect"
	"testing"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

// TestBuildItemPriorityGroupsUsesGeneratedOrder 验证 Go 直接采用生成阶段确定的售卖顺序。
func TestBuildItemPriorityGroupsUsesGeneratedOrder(t *testing.T) {
	groupsByLocation, err := buildItemPriorityGroups(testSellProductSelectionData())
	if err != nil {
		t.Fatalf("buildItemPriorityGroups: %v", err)
	}
	groups := groupsByLocation["TestOutpost"]
	if len(groups) != 2 || groups[0].ItemID != "item_b" || groups[1].ItemID != "item_a" {
		t.Fatalf("生成优先级顺序未被保留：%+v", groups)
	}
}

func TestBuildItemPriorityGroupsRejectsUnknownItem(t *testing.T) {
	data := testSellProductSelectionData()
	location := data.Locations["TestOutpost"]
	location.ItemOrder = append(location.ItemOrder, "missing")
	data.Locations["TestOutpost"] = location
	if _, err := buildItemPriorityGroups(data); err == nil {
		t.Fatal("unknown item reference should fail")
	}
}

// TestPrioritizeItemGroupsUsesConfiguredOrderBeforeDefaultOrder 验证用户配置顺序覆盖默认顺序，且不修改原切片。
func TestPrioritizeItemGroupsUsesConfiguredOrderBeforeDefaultOrder(t *testing.T) {
	original := []itemPriorityGroup{
		{ItemID: "a"},
		{ItemID: "b"},
		{ItemID: "c"},
		{ItemID: "d"},
		{ItemID: "e"},
	}
	got := prioritizeItemGroups(original, []string{"d", "missing", "b", "d", "c"})
	want := []itemPriorityGroup{
		{ItemID: "d"},
		{ItemID: "b"},
		{ItemID: "c"},
		{ItemID: "a"},
		{ItemID: "e"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("排序结果 = %+v，期望 %+v", got, want)
	}
	if !reflect.DeepEqual(original, []itemPriorityGroup{
		{ItemID: "a"},
		{ItemID: "b"},
		{ItemID: "c"},
		{ItemID: "d"},
		{ItemID: "e"},
	}) {
		t.Fatalf("原始分组被意外修改：%+v", original)
	}
}

// TestFindPriorityItemMatchUsesGroupPriorityBeforeScreenOrder 验证选品遵循售卖优先级，而不是画面中的位置顺序。
func TestFindPriorityItemMatchUsesGroupPriorityBeforeScreenOrder(t *testing.T) {
	groups := []itemPriorityGroup{
		{ItemID: "high", Candidates: []string{"高优先级"}},
		{ItemID: "low", Candidates: []string{"低优先级"}},
	}
	ocrItems := []ocrItem{
		{text: "低优先级", box: maa.Rect{100, 100, 120, 40}},
		{text: "高优先级", box: maa.Rect{100, 300, 120, 40}},
	}
	match, itemID, recognized := findPriorityItemMatch(ocrItems, groups, nil, nil, "")
	if match == nil || itemID != "high" {
		t.Fatalf("应命中高优先级物品，实际匹配 = %+v，物品 = %q", match, itemID)
	}
	if len(recognized) != 2 {
		t.Fatalf("已识别物品 = %v，期望同时包含两个物品", recognized)
	}
}

// TestFindPriorityItemMatchSkipsAttemptedAndKeepsPending 验证已尝试物品会被跳过，而待提交物品保持稳定。
func TestFindPriorityItemMatchSkipsAttemptedAndKeepsPending(t *testing.T) {
	groups := []itemPriorityGroup{
		{ItemID: "high", Candidates: []string{"高优先级"}},
		{ItemID: "low", Candidates: []string{"低优先级"}},
	}
	ocrItems := []ocrItem{
		{text: "高优先级", box: maa.Rect{100, 100, 120, 40}},
		{text: "低优先级", box: maa.Rect{100, 200, 120, 40}},
	}
	attempted := map[string]struct{}{"high": {}}
	_, itemID, _ := findPriorityItemMatch(ocrItems, groups, attempted, nil, "")
	if itemID != "low" {
		t.Fatalf("高优先级物品尝试后应选择低优先级物品，实际为 %q", itemID)
	}
	_, itemID, _ = findPriorityItemMatch(ocrItems, groups, attempted, nil, "high")
	if itemID != "high" {
		t.Fatalf("提交前应保持待选物品不变，实际为 %q", itemID)
	}
	_, itemID, _ = findPriorityItemMatch(ocrItems[1:], groups, attempted, nil, "high")
	if itemID != "" {
		t.Fatalf("待选物品暂时未识别时不应降级选择，实际为 %q", itemID)
	}
}

// TestFindPriorityItemMatchSkipsOutOfStockAcrossLocations 验证任务内缺货物品不会成为后续据点候选。
func TestFindPriorityItemMatchSkipsOutOfStockAcrossLocations(t *testing.T) {
	groups := []itemPriorityGroup{
		{ItemID: "high", Candidates: []string{"高优先级"}},
		{ItemID: "low", Candidates: []string{"低优先级"}},
	}
	ocrItems := []ocrItem{
		{text: "高优先级", box: maa.Rect{100, 100, 120, 40}},
		{text: "低优先级", box: maa.Rect{100, 200, 120, 40}},
	}
	outOfStock := map[string]struct{}{"high": {}}
	_, itemID, recognized := findPriorityItemMatch(ocrItems, groups, nil, outOfStock, "")
	if itemID != "low" {
		t.Fatalf("高优先级物品缺货后应选择低优先级物品，实际为 %q", itemID)
	}
	if !reflect.DeepEqual(recognized, []string{"high", "low"}) {
		t.Fatalf("缺货物品仍应保留在稳定识别集合中，实际为 %v", recognized)
	}
}
