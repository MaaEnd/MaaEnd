package sellproduct

import (
	"strings"
	"testing"

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
			expected: []string{"恢复干员", "测试干员", "TestLocation"},
		},
		{
			name:     "货品切换",
			message:  runtimeItemSwitchedMessage("TestLocation", "test_item"),
			expected: []string{"test_item", "TestLocation"},
		},
		{
			name:     "全量缓存扫描失败",
			message:  runtimeOperatorScanFailedMessage("global", operatorActionUsageAll),
			expected: []string{"干员缓存扫描失败"},
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
	})

	for _, expected := range []string{
		"测试据点",
		"售卖干员",
		"恢复干员",
		"物品甲 → 物品乙",
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
