package seizedeliveryjobs

import (
	"testing"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

func TestReadMaxAttemptRounds(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want int
	}{
		{"empty falls back", "", defaultMaxAttemptRounds},
		{"json number", `{"max_attempt_rounds": 30}`, 30},
		{"json string", `{"max_attempt_rounds": "30"}`, 30},
		{"zero falls back", `{"max_attempt_rounds": 0}`, defaultMaxAttemptRounds},
		{"negative falls back", `{"max_attempt_rounds": -5}`, defaultMaxAttemptRounds},
		{"non numeric string falls back", `{"max_attempt_rounds": "abc"}`, defaultMaxAttemptRounds},
		{"missing key falls back", `{}`, defaultMaxAttemptRounds},
		{"malformed json falls back", `{`, defaultMaxAttemptRounds},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := readMaxAttemptRounds(c.raw); got != c.want {
				t.Fatalf("readMaxAttemptRounds(%q) = %d, want %d", c.raw, got, c.want)
			}
		})
	}
}

// TestNoProgressActionTerminates 覆盖 #5707 的核心诉求：
// 连续未接到委托未达上限时继续刷新（返回 true），达到上限后必须终止（返回 false）。
func TestNoProgressActionTerminates(t *testing.T) {
	const limit = 3
	arg := &maa.CustomActionArg{CustomActionParam: `{"max_attempt_rounds": 3}`}
	action := &SeizeDeliveryJobsNoProgressAction{}

	resetScanState()
	for round := 1; round <= limit; round++ {
		got := action.Run(nil, arg)
		want := round < limit
		if got != want {
			t.Fatalf("round %d: Run() = %v, want %v (attemptRounds=%d)", round, got, want, attemptRounds)
		}
	}
	if attemptRounds != limit {
		t.Fatalf("attemptRounds = %d, want %d", attemptRounds, limit)
	}
}

// TestAttemptRoundsSurviveRoundCleanup 覆盖「终点匹配成功但接取失败」这条路径：
// 轮次之间的单轮状态清理不得把跨轮计数清零，否则该循环永远达不到上限。
func TestAttemptRoundsSurviveRoundCleanup(t *testing.T) {
	const limit = 5
	arg := &maa.CustomActionArg{CustomActionParam: `{"max_attempt_rounds": 5}`}
	action := &SeizeDeliveryJobsNoProgressAction{}

	resetScanState()
	for round := 1; round <= limit; round++ {
		// 模拟一轮：扫描拿到数据 → 命中终点 → 清理单轮状态 → 接取失败 → 计数
		scannedJobItems = []deliveryJobItem{{OriginText: "试验园区"}}
		currentIndex = 1
		clearRoundState()
		if attemptRounds != round-1 {
			t.Fatalf("round %d: clearRoundState changed the counter to %d, want %d", round, attemptRounds, round-1)
		}
		got := action.Run(nil, arg)
		want := round < limit
		if got != want {
			t.Fatalf("round %d: Run() = %v, want %v (attemptRounds=%d)", round, got, want, attemptRounds)
		}
	}
}

// TestResetScanStateIsTheOnlyResetPoint 确认只有任务入口动作会把计数归零。
func TestResetScanStateIsTheOnlyResetPoint(t *testing.T) {
	arg := &maa.CustomActionArg{CustomActionParam: `{"max_attempt_rounds": 10}`}
	noProgress := &SeizeDeliveryJobsNoProgressAction{}
	reset := &SeizeDeliveryJobsResetScanStateAction{}

	resetScanState()
	noProgress.Run(nil, arg)
	noProgress.Run(nil, arg)
	if attemptRounds != 2 {
		t.Fatalf("attemptRounds = %d, want 2", attemptRounds)
	}

	clearRoundState()
	if attemptRounds != 2 {
		t.Fatalf("clearRoundState cleared the cross-round counter: %d, want 2", attemptRounds)
	}

	reset.Run(nil, nil)
	if attemptRounds != 0 {
		t.Fatalf("reset did not clear the counter: %d, want 0", attemptRounds)
	}
	if scannedJobItems != nil || currentIndex != 0 {
		t.Fatalf("reset left round state: items=%v index=%d", scannedJobItems, currentIndex)
	}
}
