package goods

import (
	"reflect"
	"strings"
	"testing"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
	sellstrategy "github.com/MaaXYZ/MaaEnd/agent/go-service/sellproduct/goods/strategy"
)

func TestRuntimeMessagesContainCurrentGoodsState(t *testing.T) {
	i18n.Init()
	tests := []struct {
		name     string
		message  string
		expected []string
	}{
		{
			name:     "货品切换",
			message:  runtimeItemSwitchedMessage("TestLocation", "test_item"),
			expected: []string{"test_item", "TestLocation"},
		},
		{
			name:     "货品缺货排除",
			message:  runtimeItemOutOfStockMessage("TestLocation", "test_item"),
			expected: []string{"缺货", "test_item", "TestLocation"},
		},
		{
			name:     "保留量已满足",
			message:  runtimeReserveSatisfiedMessage("test_item", 1000),
			expected: []string{"test_item", "保留数量 1000", "后续据点将跳过"},
		},
		{
			name:     "仅售卖优先产品",
			message:  runtimeOnlyPreferredEnabledMessage(),
			expected: []string{"仅售卖优先产品", "其他产品不会售卖", "已开启地区优先售卖配置"},
		},
		{
			name:     "库存优先售卖",
			message:  runtimeStockPriorityEnabledMessage(3),
			expected: []string{"库存优先售卖", "本地仓储", "单价不低于 3"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			for _, expected := range test.expected {
				if !strings.Contains(test.message, expected) {
					t.Fatalf("运行消息 %q 不包含 %q", test.message, expected)
				}
			}
		})
	}
}

func TestOrderLocationPlanGroupsUsesStaticStrategy(t *testing.T) {
	groups := []itemPriorityGroup{
		{ItemID: "low", Rarity: 2, UnitPrice: 100},
		{ItemID: "high_cheap", Rarity: 3, UnitPrice: 80},
		{ItemID: "high_expensive", Rarity: 3, UnitPrice: 120},
	}
	tests := []struct {
		kind sellstrategy.Kind
		want []string
	}{
		{kind: sellstrategy.KindRarity, want: []string{"high_expensive", "high_cheap", "low"}},
		{kind: sellstrategy.KindPrice, want: []string{"high_expensive", "low", "high_cheap"}},
	}
	for _, test := range tests {
		ordered := orderLocationPlanGroups(groups, test.kind, sellstrategy.Config{})
		if got := []string{ordered[0].ItemID, ordered[1].ItemID, ordered[2].ItemID}; !reflect.DeepEqual(got, test.want) {
			t.Fatalf("%s 计划顺序 = %v，期望 %v", test.kind, got, test.want)
		}
	}
}

// TestBuildRuntimeLocationPlanItemsSeparatesOutOfStock 验证只排除当前据点支持的缺货物品，并保持计划顺序。
func TestBuildRuntimeLocationPlanItemsSeparatesOutOfStock(t *testing.T) {
	groups := []itemPriorityGroup{
		{ItemID: "item_a"},
		{ItemID: "item_b"},
	}
	items, excludedOutOfStock, reserveSatisfied, excludedByUser := buildRuntimeLocationPlanItems(
		groups,
		map[string]int{"item_a": 10, "item_b": 20},
		map[string]struct{}{"item_b": {}, "other_location_item": {}},
		nil,
	)
	if len(items) != 1 || items[0].Name != "item_a" || items[0].ReserveQuantity != 10 {
		t.Fatalf("可售计划 = %+v，期望仅保留 item_a 及其保留规则", items)
	}
	if len(excludedOutOfStock) != 1 || excludedOutOfStock[0] != "item_b" {
		t.Fatalf("缺货排除 = %v，期望仅包含当前据点的 item_b", excludedOutOfStock)
	}
	if len(excludedByUser) != 0 {
		t.Fatalf("用户排除 = %v，期望为空", excludedByUser)
	}
	if len(reserveSatisfied) != 0 {
		t.Fatalf("保留量已满足 = %v，期望为空", reserveSatisfied)
	}
}

func TestBuildRuntimeLocationPlanItemsSeparatesBlacklist(t *testing.T) {
	groups := []itemPriorityGroup{
		{ItemID: "item_a"},
		{ItemID: "item_b"},
	}
	items, excludedOutOfStock, reserveSatisfied, excludedByUser := buildRuntimeLocationPlanItems(
		groups,
		map[string]int{"item_a": reserveBlacklistQuantity, "item_b": 20},
		map[string]struct{}{"item_a": {}},
		nil,
	)
	if len(items) != 1 || items[0].Name != "item_b" || items[0].ReserveQuantity != 20 {
		t.Fatalf("可售计划 = %+v，期望仅包含 item_b", items)
	}
	if len(excludedOutOfStock) != 0 {
		t.Fatalf("黑名单物品不应被记为缺货：%v", excludedOutOfStock)
	}
	if !reflect.DeepEqual(excludedByUser, []string{"item_a"}) {
		t.Fatalf("用户排除 = %v，期望包含 item_a", excludedByUser)
	}
	if len(reserveSatisfied) != 0 {
		t.Fatalf("用户黑名单物品不应记为已满足保留量：%v", reserveSatisfied)
	}
}

func TestBuildRuntimeLocationPlanItemsSeparatesSatisfiedReserve(t *testing.T) {
	groups := []itemPriorityGroup{
		{ItemID: "item_a"},
		{ItemID: "item_b"},
	}
	items, excludedOutOfStock, reserveSatisfied, excludedByUser := buildRuntimeLocationPlanItems(
		groups,
		map[string]int{"item_a": 10, "item_b": 20},
		nil,
		map[string]struct{}{"item_a": {}, "other_location_item": {}},
	)
	if len(items) != 1 || items[0].Name != "item_b" {
		t.Fatalf("可售计划 = %+v，期望仅包含 item_b", items)
	}
	if !reflect.DeepEqual(reserveSatisfied, []LocationPlanItem{{Name: "item_a", ReserveQuantity: 10}}) {
		t.Fatalf("保留量已满足 = %+v，期望包含 item_a 及其保留数量", reserveSatisfied)
	}
	if len(excludedOutOfStock) != 0 || len(excludedByUser) != 0 {
		t.Fatalf("已满足保留量不应混入其他排除原因：缺货=%v 用户=%v", excludedOutOfStock, excludedByUser)
	}
}
