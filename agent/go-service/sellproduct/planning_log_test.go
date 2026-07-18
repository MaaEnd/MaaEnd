package sellproduct

import (
	"strings"
	"testing"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
)

func TestComposePlanningLogSnapshot(t *testing.T) {
	operatorData := &operatorSelectionData{
		TargetCandidates: map[string][]operatorCandidate{
			"A": {{Name: "TargetA", CacheName: "售卖甲", DisplayName: "售卖甲", Priority: 0}},
			"B": {{Name: "TargetB", CacheName: "售卖乙", DisplayName: "售卖乙", Priority: 0}},
		},
		LocationNames: map[string]string{"A": "据点甲", "B": "据点乙"},
		RestoreGroups: []operatorCandidateGroup{
			{Location: "A", Candidates: []operatorCandidate{
				{Name: "RestoreA", CacheName: "恢复甲", DisplayName: "恢复甲", Priority: 0},
				{Name: "TargetA", CacheName: "售卖甲", DisplayName: "售卖甲", Priority: 1},
			}},
			{Location: "B", Candidates: []operatorCandidate{{Name: "RestoreB", CacheName: "恢复乙", DisplayName: "恢复乙", Priority: 0}}},
		},
	}
	ownership := operatorOwnership{
		Operators: operatorNameSet([]string{"售卖甲", "售卖乙", "恢复甲", "恢复乙"}),
	}
	session := operatorSessionState{
		UID:             "123456789",
		Mode:            operatorCacheModeCache,
		ActiveLocations: map[string]struct{}{"B": {}, "A": {}},
	}
	itemPriorities := map[string][]itemPriorityGroup{
		"A": {
			{ItemID: "item_high", DisplayName: "高级货品", Candidates: []string{"高级货品"}},
			{ItemID: "item_low", DisplayName: "低级货品", Candidates: []string{"低级货品"}},
		},
		"B": {{ItemID: "item_b", DisplayName: "乙货品", Candidates: []string{"乙货品"}}},
	}

	got := composePlanningLogSnapshot(operatorData, ownership, session, itemPriorities, []string{
		"item_low",
	}, map[string]int{
		"item_low": 10,
	})
	if len(got.Operators) != 2 || got.Operators[0].Location != "A" || got.Operators[1].Location != "B" {
		t.Fatalf("operator locations are not stable: %+v", got.Operators)
	}
	if got.Operators[0].TargetOperator != "售卖甲" || got.Operators[0].RestoreOperator != "售卖甲" {
		t.Fatalf("unexpected operator plan for A: %+v", got.Operators[0])
	}
	if len(got.ItemPriorities) != 2 || got.ItemPriorities[0].Items[1].Priority != 2 {
		t.Fatalf("unexpected item priorities: %+v", got.ItemPriorities)
	}
	if got.ItemPriorities[0].Items[0].ItemID != "item_low" {
		t.Fatalf("preferred item was not moved to the front: %+v", got.ItemPriorities[0].Items)
	}
	if len(got.ReserveRules) != 1 || got.ReserveRules[0].Name != "低级货品" || got.ReserveRules[0].Quantity != 10 {
		t.Fatalf("unexpected reserve rules: %+v", got.ReserveRules)
	}
}

func TestOperatorSessionClaimPlanningLogOnce(t *testing.T) {
	resetOperatorSessionForTest(t, operatorCacheModeCache)
	if !operatorSessionClaimPlanningLog() {
		t.Fatal("first planning log claim should succeed")
	}
	if operatorSessionClaimPlanningLog() {
		t.Fatal("second planning log claim should be ignored")
	}
}

func TestOrderedActiveLocations(t *testing.T) {
	got := orderedActiveLocations([]string{"B", "A"}, map[string]struct{}{
		"C": {},
		"A": {},
		"B": {},
	})
	want := []string{"B", "A", "C"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("ordered locations = %v, want %v", got, want)
	}
}

func TestPlanningFocusMessages(t *testing.T) {
	i18n.Init()
	snapshot := planningLogSnapshot{
		Operators: []planningOperatorEntry{
			{
				Location:        "A",
				LocationName:    "测试据点",
				TargetOperator:  "售卖干员",
				RestoreOperator: "恢复干员",
			},
		},
		ItemPriorities: []planningLocationItems{
			{
				Location:     "A",
				LocationName: "测试据点",
				Items: []planningItemEntry{
					{Priority: 1, ItemID: "item_a", Name: "物品甲"},
					{Priority: 2, ItemID: "item_b", Name: "物品乙"},
				},
			},
		},
		ReserveRules: []planningReserveRule{
			{ItemID: "item_b", Name: "物品乙", Quantity: 10},
			{ItemID: "item_elsewhere", Name: "其他物品", Quantity: 20},
		},
	}

	messages := planningFocusMessages(snapshot)
	if len(messages) != 2 {
		t.Fatalf("focus message count = %d, want 2", len(messages))
	}
	for _, expected := range []string{
		"测试据点",
		"售卖干员",
		"恢复干员",
		"物品甲 → 物品乙",
		"物品乙 ≥ 10",
	} {
		if !strings.Contains(messages[1], expected) {
			t.Fatalf("focus message %q does not contain %q", messages[1], expected)
		}
	}
	if strings.Contains(messages[1], "其他物品") {
		t.Fatalf("focus message contains reserve rule from another outpost: %q", messages[1])
	}
}
