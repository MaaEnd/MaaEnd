package sellproduct

import (
	"strings"
	"testing"
	"time"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
)

func TestRuntimeMessagesContainCurrentState(t *testing.T) {
	i18n.Init()
	candidate := operatorCandidate{DisplayName: "测试干员"}

	tests := []struct {
		name     string
		message  string
		expected []string
	}{
		{
			name:     "干员切换",
			message:  runtimeOperatorAssignmentMessage("TestLocation", operatorActionUsageTarget, candidate, true),
			expected: []string{"售卖干员", "测试干员", "TestLocation"},
		},
		{
			name:     "完整扫描后重新规划",
			message:  runtimeOperatorReplannedMessage("TestLocation", operatorActionUsageRestore, candidate),
			expected: []string{"售后生产干员", "测试干员", "TestLocation"},
		},
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
			name:     "全量缓存扫描失败",
			message:  runtimeOperatorScanFailedMessage("global", operatorActionUsageAll),
			expected: []string{"干员缓存扫描失败"},
		},
		{
			name: "加载干员缓存",
			message: runtimeOperatorCacheStatusMessage(operatorCacheStatus{
				Ready:     true,
				UpdatedAt: time.Date(2026, 7, 20, 2, 56, 13, 0, time.UTC),
			}),
			expected: []string{
				"已加载干员列表缓存",
				time.Date(2026, 7, 20, 2, 56, 13, 0, time.UTC).Local().Format("2006-01-02 15:04:05"),
			},
		},
		{
			name:     "扫描干员缓存",
			message:  runtimeOperatorCacheStatusMessage(operatorCacheStatus{}),
			expected: []string{"正在扫描并缓存干员列表"},
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

func TestRuntimeLocalCacheUpdatedAtFallsBackForInvalidTimestamp(t *testing.T) {
	i18n.Init()
	if got := runtimeLocalCacheUpdatedAt(time.Time{}); got != "未知" {
		t.Fatalf("无效缓存时间 = %q，期望未知", got)
	}
}

func TestRuntimeLocationPlanMessage(t *testing.T) {
	i18n.Init()
	message := runtimeLocationPlanMessage(runtimeLocationPlan{
		LocationName:    "测试据点",
		TargetOperator:  "售卖干员",
		RestoreOperator: "恢复干员",
		Items: []runtimeLocationPlanItem{
			{Name: "物品甲"},
			{Name: "物品乙", ReserveQuantity: 10},
		},
		ExcludedOutOfStock: []string{"物品丙"},
	})

	for _, expected := range []string{
		"测试据点",
		"售卖干员",
		"恢复干员",
		"物品甲 → 物品乙",
		"缺货排除：物品丙",
		"物品乙保留 10",
	} {
		if !strings.Contains(message, expected) {
			t.Fatalf("据点计划 %q 不包含 %q", message, expected)
		}
	}
	if strings.Contains(message, "物品甲保留") {
		t.Fatalf("据点计划错误显示了未配置的保留规则：%q", message)
	}
}

// TestBuildRuntimeLocationPlanItemsSeparatesOutOfStock 验证只排除当前据点支持的缺货物品，并保持计划顺序。
func TestBuildRuntimeLocationPlanItemsSeparatesOutOfStock(t *testing.T) {
	groups := []itemPriorityGroup{
		{ItemID: "item_a", DisplayName: "物品甲"},
		{ItemID: "item_b", DisplayName: "物品乙"},
	}
	items, excluded := buildRuntimeLocationPlanItems(
		groups,
		map[string]int{"item_a": 10, "item_b": 20},
		map[string]struct{}{"item_b": {}, "other_location_item": {}},
	)
	if len(items) != 1 || items[0].Name != "物品甲" || items[0].ReserveQuantity != 10 {
		t.Fatalf("可售计划 = %+v，期望仅保留物品甲及其保留规则", items)
	}
	if len(excluded) != 1 || excluded[0] != "物品乙" {
		t.Fatalf("缺货排除 = %v，期望仅包含当前据点的物品乙", excluded)
	}
}

func TestRuntimeLocationPlanMessageWithoutReserve(t *testing.T) {
	i18n.Init()
	message := runtimeLocationPlanMessage(runtimeLocationPlan{
		LocationName: "测试据点",
		Items:        []runtimeLocationPlanItem{{Name: "物品甲"}},
	})

	for _, expected := range []string{"无", "全部售卖"} {
		if !strings.Contains(message, expected) {
			t.Fatalf("无保留计划 %q 不包含 %q", message, expected)
		}
	}
}
