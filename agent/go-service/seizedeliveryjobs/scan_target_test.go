package seizedeliveryjobs

import "testing"

// TestNoProgressActionTerminates 覆盖 #5707 的核心诉求：
// 连续未接到委托未达上限时继续刷新（返回 true），达到 maxAttemptRounds 后终止（返回 false）。
func TestNoProgressActionTerminates(t *testing.T) {
	action := &SeizeDeliveryJobsNoProgressAction{}

	resetScanState()
	for round := 1; round <= maxAttemptRounds; round++ {
		got := action.Run(nil, nil)
		want := round < maxAttemptRounds
		if got != want {
			t.Fatalf("round %d: Run() = %v, want %v (attemptRounds=%d)", round, got, want, attemptRounds)
		}
	}
	if attemptRounds != maxAttemptRounds {
		t.Fatalf("attemptRounds = %d, want %d", attemptRounds, maxAttemptRounds)
	}
}

// TestAttemptRoundsSurviveRoundCleanup 覆盖「终点匹配成功但接取失败」这条路径：
// 轮次之间的单轮状态清理不得把跨轮计数清零，否则该循环永远达不到上限。
func TestAttemptRoundsSurviveRoundCleanup(t *testing.T) {
	action := &SeizeDeliveryJobsNoProgressAction{}

	resetScanState()
	for round := 1; round <= maxAttemptRounds; round++ {
		// 模拟一轮：扫描拿到数据 → 命中终点 → 清理单轮状态 → 接取失败 → 计数
		scannedJobItems = []deliveryJobItem{{OriginText: "试验园区"}}
		currentIndex = 1
		clearRoundState()
		if attemptRounds != round-1 {
			t.Fatalf("round %d: clearRoundState changed the counter to %d, want %d", round, attemptRounds, round-1)
		}
		got := action.Run(nil, nil)
		want := round < maxAttemptRounds
		if got != want {
			t.Fatalf("round %d: Run() = %v, want %v (attemptRounds=%d)", round, got, want, attemptRounds)
		}
	}
}

// TestResetScanStateIsTheOnlyResetPoint 确认只有任务入口动作会把计数归零。
func TestResetScanStateIsTheOnlyResetPoint(t *testing.T) {
	noProgress := &SeizeDeliveryJobsNoProgressAction{}
	reset := &SeizeDeliveryJobsResetScanStateAction{}

	resetScanState()
	noProgress.Run(nil, nil)
	noProgress.Run(nil, nil)
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
