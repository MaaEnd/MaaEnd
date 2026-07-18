package sellproduct

import (
	"testing"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

// TestBuildItemSelectionGroupsUsesGeneratedCatalog 验证 Go 按生成数据的稳定顺序展开物品名称。
func TestBuildItemSelectionGroupsUsesGeneratedCatalog(t *testing.T) {
	groups, err := buildItemSelectionGroups(testSellProductSelectionData())
	if err != nil {
		t.Fatalf("buildItemSelectionGroups: %v", err)
	}
	if len(groups) != 2 {
		t.Fatalf("物品分组数量 = %d，期望 2", len(groups))
	}
	if groups[0].ItemID != "item_a" || len(groups[0].Candidates) != 2 {
		t.Fatalf("生成物品分组不符合预期：%+v", groups[0])
	}
}

// TestFindBestItemMatchReturnsStableItemID 验证 OCR 名称命中后能还原为稳定的物品 ID。
func TestFindBestItemMatchReturnsStableItemID(t *testing.T) {
	groups := []itemSelectionGroup{
		{ItemID: "item_a", Candidates: []string{"物品甲", "Item A"}},
		{ItemID: "item_b", Candidates: []string{"物品乙", "Item B"}},
	}
	match, itemID := findBestItemMatch([]ocrItem{{
		text: "I物品乙",
		box:  maa.Rect{20, 30, 100, 20},
	}}, groups)
	if match == nil || itemID != "item_b" {
		t.Fatalf("应命中 item_b，实际匹配 = %+v，物品 = %q", match, itemID)
	}
}
