package sellproduct

import (
	"testing"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

func TestBuildItemSelectionGroupsMergesItemID(t *testing.T) {
	data := settlementTradeFile{Settlements: map[string]settlementTradeSettlement{
		"stm_tundra_1": {
			DomainID: "domain_1",
			ByProsperityLevel: map[string]settlementTradeProsperityLevel{
				"1": {TradeItems: []settlementTradeItem{{
					ItemID: "item_a",
					Name:   map[string]string{"CN": "物品甲", "EN": "Item A"},
				}}},
				"2": {TradeItems: []settlementTradeItem{{
					ItemID: "item_a",
					Name:   map[string]string{"TC": "物品甲繁", "KR": "아이템 A"},
				}}},
			},
		},
	}}
	groups := buildItemSelectionGroups(data)
	if len(groups) != 1 {
		t.Fatalf("expected one merged group, got %d", len(groups))
	}
	if groups[0].ItemID != "item_a" || len(groups[0].Candidates) != 4 {
		t.Fatalf("unexpected merged group: %+v", groups[0])
	}
}

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
		t.Fatalf("expected item_b match, got match=%+v item=%q", match, itemID)
	}
}
