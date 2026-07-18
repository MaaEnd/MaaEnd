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
