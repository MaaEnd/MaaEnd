package goods

import (
	"reflect"
	"testing"

	sellstrategy "github.com/MaaXYZ/MaaEnd/agent/go-service/sellproduct/goods/strategy"
)

// TestPrioritizeItemGroupsUsesConfiguredOrderBeforeDefaultOrder 验证用户配置顺序覆盖默认顺序，且不修改原切片。
func TestPrioritizeItemGroupsUsesConfiguredOrderBeforeDefaultOrder(t *testing.T) {
	original := []itemPriorityGroup{
		{ItemID: "a"},
		{ItemID: "b"},
		{ItemID: "c"},
		{ItemID: "d"},
		{ItemID: "e"},
	}
	got := prioritizeItemGroups(original, []string{"d", "missing", "b", "d", "c"}, false)
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

// TestPrioritizeItemGroupsCanExcludeUnconfiguredItems 验证严格优先模式只保留当前据点可售、
// 且由用户明确配置的物品。
func TestPrioritizeItemGroupsCanExcludeUnconfiguredItems(t *testing.T) {
	original := []itemPriorityGroup{
		{ItemID: "a"},
		{ItemID: "b"},
		{ItemID: "c"},
	}
	got := prioritizeItemGroups(original, []string{"c", "missing", "a", "c"}, true)
	want := []itemPriorityGroup{
		{ItemID: "c"},
		{ItemID: "a"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("严格优先模式排序结果 = %+v，期望 %+v", got, want)
	}
}

func TestSelectGoodsTargetAppliesMinimumPriceAndExistingExclusions(t *testing.T) {
	resetReserveSession()
	groups := []itemPriorityGroup{
		{ItemID: "cheap", UnitPrice: 2},
		{ItemID: "blocked", UnitPrice: 70},
		{ItemID: "eligible", UnitPrice: 30},
	}
	observations := map[string]sellstrategy.Candidate{
		"cheap":    {ItemID: "cheap", Stock: 99999, StockKnown: true, UnitPrice: 2},
		"blocked":  {ItemID: "blocked", Stock: 88888, StockKnown: true, UnitPrice: 70},
		"eligible": {ItemID: "eligible", Stock: 100, StockKnown: true, UnitPrice: 30},
	}
	registerReserveRule("blocked", reserveBlacklistQuantity)
	itemID, recognized := selectGoodsTarget(
		"Outpost",
		groups,
		prioritySelectionPolicy{
			Strategy:       sellstrategy.KindStock,
			StrategyConfig: sellstrategy.Config{MinimumUnitPrice: 3},
		},
		observations,
	)
	if itemID != "eligible" {
		t.Fatalf("filtered stock target = %q, want eligible", itemID)
	}
	if !reflect.DeepEqual(recognized, []string{"blocked", "cheap", "eligible"}) {
		t.Fatalf("recognized items = %v", recognized)
	}
}

func TestSelectGoodsTargetStaticModesUseConfiguredOrderAndSkipZeroStock(t *testing.T) {
	resetReserveSession()
	groups := []itemPriorityGroup{
		{ItemID: "zero", Rarity: 5, UnitPrice: 70},
		{ItemID: "low_expensive", Rarity: 2, UnitPrice: 100},
		{ItemID: "high", Rarity: 3, UnitPrice: 30},
		{ItemID: "more_stock", Rarity: 3, UnitPrice: 10},
	}
	observations := map[string]sellstrategy.Candidate{
		"zero":          {ItemID: "zero", Stock: 0, StockKnown: true, Rarity: 5, UnitPrice: 70},
		"low_expensive": {ItemID: "low_expensive", Stock: 100, StockKnown: true, Rarity: 2, UnitPrice: 100},
		"high":          {ItemID: "high", Stock: 10, StockKnown: true, Rarity: 3, UnitPrice: 30},
		"more_stock":    {ItemID: "more_stock", Stock: 99999, StockKnown: true, Rarity: 3, UnitPrice: 10},
	}
	tests := []struct {
		kind sellstrategy.Kind
		want string
	}{
		{kind: sellstrategy.KindRarity, want: "high"},
		{kind: sellstrategy.KindPrice, want: "low_expensive"},
	}
	for _, test := range tests {
		itemID, _ := selectGoodsTarget(
			"Outpost",
			groups,
			prioritySelectionPolicy{Strategy: test.kind},
			observations,
		)
		if itemID != test.want {
			t.Fatalf("%s mode target = %q, want %q", test.kind, itemID, test.want)
		}
	}
}

func TestSelectGoodsTargetKeepsNameOnlyItemForStaticStrategies(t *testing.T) {
	resetReserveSession()
	groups := []itemPriorityGroup{
		{ItemID: "known", Rarity: 2, UnitPrice: 30},
		{ItemID: "name_only", Rarity: 4, UnitPrice: 100},
	}
	observations := map[string]sellstrategy.Candidate{
		"known":     {ItemID: "known", Stock: 100, StockKnown: true, Rarity: 2, UnitPrice: 30},
		"name_only": {ItemID: "name_only", Rarity: 4, UnitPrice: 100},
	}
	tests := []struct {
		kind sellstrategy.Kind
		want string
	}{
		{kind: sellstrategy.KindRarity, want: "name_only"},
		{kind: sellstrategy.KindPrice, want: "name_only"},
		{kind: sellstrategy.KindStock, want: "known"},
	}
	for _, test := range tests {
		itemID, _ := selectGoodsTarget(
			"Outpost",
			groups,
			prioritySelectionPolicy{Strategy: test.kind},
			observations,
		)
		if itemID != test.want {
			t.Fatalf("%s mode target = %q, want %q", test.kind, itemID, test.want)
		}
	}
}

func TestSelectGoodsTargetUsesEligibleManualPriorityBeforeStrategy(t *testing.T) {
	resetReserveSession()
	groups := []itemPriorityGroup{
		{ItemID: "other", Rarity: 3, UnitPrice: 70},
		{ItemID: "cheap", Rarity: 2, UnitPrice: 2},
		{ItemID: "preferred", Rarity: 2, UnitPrice: 20},
	}
	observations := map[string]sellstrategy.Candidate{
		"other":     {ItemID: "other", Stock: 99999, StockKnown: true, Rarity: 3, UnitPrice: 70},
		"cheap":     {ItemID: "cheap", Stock: 1000, StockKnown: true, Rarity: 2, UnitPrice: 2},
		"preferred": {ItemID: "preferred", Stock: 10, StockKnown: true, Rarity: 2, UnitPrice: 20},
	}
	itemID, _ := selectGoodsTarget(
		"Outpost",
		groups,
		prioritySelectionPolicy{
			Preferred:      []string{"cheap", "preferred"},
			Strategy:       sellstrategy.KindStock,
			StrategyConfig: sellstrategy.Config{MinimumUnitPrice: 10},
		},
		observations,
	)
	if itemID != "preferred" {
		t.Fatalf("manual priority target = %q, want preferred", itemID)
	}
}

func TestSelectGoodsTargetKeepsExistingExclusions(t *testing.T) {
	resetReserveSession()
	groups := []itemPriorityGroup{
		{ItemID: "attempted"},
		{ItemID: "out_of_stock"},
		{ItemID: "satisfied"},
		{ItemID: "eligible"},
	}
	observations := map[string]sellstrategy.Candidate{}
	for _, group := range groups {
		observations[group.ItemID] = sellstrategy.Candidate{ItemID: group.ItemID, Stock: 100, StockKnown: true}
	}
	prioritySelectionMu.Lock()
	prioritySelection.Attempted["Outpost"] = map[string]struct{}{"attempted": {}}
	prioritySelection.OutOfStock["out_of_stock"] = struct{}{}
	prioritySelectionMu.Unlock()
	reserveSessionMu.Lock()
	reserveSatisfiedItems["satisfied"] = struct{}{}
	reserveSessionMu.Unlock()

	itemID, recognized := selectGoodsTarget(
		"Outpost",
		groups,
		prioritySelectionPolicy{Strategy: sellstrategy.KindRarity},
		observations,
	)
	if itemID != "eligible" {
		t.Fatalf("filtered normal mode target = %q, want eligible", itemID)
	}
	if !reflect.DeepEqual(recognized, []string{"attempted", "eligible", "out_of_stock", "satisfied"}) {
		t.Fatalf("recognized items = %v", recognized)
	}
}

func TestSelectGoodsTargetDoesNotReplacePendingItemOnZeroOCR(t *testing.T) {
	resetReserveSession()
	prioritySelectionMu.Lock()
	prioritySelection.Pending["Outpost"] = "pending"
	prioritySelectionMu.Unlock()
	groups := []itemPriorityGroup{{ItemID: "pending"}, {ItemID: "fallback"}}
	observations := map[string]sellstrategy.Candidate{
		"pending":  {ItemID: "pending", Stock: 0, StockKnown: true},
		"fallback": {ItemID: "fallback", Stock: 100, StockKnown: true},
	}

	itemID, _ := selectGoodsTarget(
		"Outpost",
		groups,
		prioritySelectionPolicy{Strategy: sellstrategy.KindRarity},
		observations,
	)
	if itemID != "" {
		t.Fatalf("pending zero-stock OCR must retry instead of selecting %q", itemID)
	}
}

func TestBuildStockObservationsOnlyIncludesVisiblePageItems(t *testing.T) {
	observations := buildStockObservations(
		[]stockPageItem{{ItemID: "visible", Quantity: 100, StockKnown: true}},
		[]itemPriorityGroup{
			{ItemID: "visible", Rarity: 3, UnitPrice: 10},
			{ItemID: "second_page", UnitPrice: 70},
		},
	)
	if !reflect.DeepEqual(observations, map[string]sellstrategy.Candidate{
		"visible": {ItemID: "visible", Stock: 100, StockKnown: true, Rarity: 3, UnitPrice: 10},
	}) {
		t.Fatalf("first-page observations = %+v", observations)
	}
}
