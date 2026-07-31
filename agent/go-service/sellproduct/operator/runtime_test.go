package operator

import (
	"strings"
	"testing"
	"time"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
)

func TestRuntimeMessagesContainCurrentState(t *testing.T) {
	i18n.Init()
	candidate := operatorCandidate{Name: "测试干员"}

	tests := []struct {
		name     string
		message  string
		expected []string
	}{
		{
			name:     "干员切换",
			message:  runtimeOperatorAssignmentMessage("TestLocation", operatorUsageTarget, candidate, true),
			expected: []string{"售卖干员", "测试干员", "TestLocation"},
		},
		{
			name:     "完整扫描后重新规划",
			message:  runtimeOperatorReplannedMessage("TestLocation", operatorUsageRestore, candidate),
			expected: []string{"售后生产干员", "测试干员", "TestLocation"},
		},
		{
			name:     "全量缓存扫描失败",
			message:  runtimeOperatorScanFailedMessage("global", operatorUsageAll),
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
