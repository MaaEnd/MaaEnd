package goods

import "testing"

// TestPriorityExhaustionAllowsStableEmptySetInOnlyPreferredMode 验证严格优先模式下，
// 当前地区没有任何已配置候选时会在两次稳定观察后正常结束，而不是识别超时。
func TestPriorityExhaustionAllowsStableEmptySetInOnlyPreferredMode(t *testing.T) {
	resetPrioritySelectionSession()
	if result, ok := buildPriorityExhaustedResult("Outpost", nil); ok || result != nil {
		t.Fatalf("首次空集合观察不应立即结束：result = %+v, ok = %v", result, ok)
	}
	result, ok := buildPriorityExhaustedResult("Outpost", nil)
	if !ok || result == nil {
		t.Fatalf("第二次空集合观察应确认耗尽：result = %+v, ok = %v", result, ok)
	}
}
