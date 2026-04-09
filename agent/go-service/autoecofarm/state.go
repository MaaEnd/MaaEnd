package autoecofarm

import (
	"sync"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

var (
	stateMu              sync.RWMutex
	lastSwipeTargetState *swipeTargetState
)

type swipeTargetState struct {
	lastRoi    maa.Rect
	xStepRatio float64
	yStepRatio float64
}

// getLastState 返回最近一次缓存状态的快照。
func getLastState() *swipeTargetState {
	stateMu.RLock()
	defer stateMu.RUnlock()

	if lastSwipeTargetState == nil {
		return nil
	}

	return &swipeTargetState{
		lastRoi:    lastSwipeTargetState.lastRoi,
		xStepRatio: lastSwipeTargetState.xStepRatio,
		yStepRatio: lastSwipeTargetState.yStepRatio,
	}
}

// setLastState 保存最新的识别 ROI 和 StepRatio。
func setLastState(roi maa.Rect, xStepRatio, yStepRatio float64) {
	stateMu.Lock()
	defer stateMu.Unlock()

	lastSwipeTargetState = &swipeTargetState{
		lastRoi:    roi,
		xStepRatio: xStepRatio,
		yStepRatio: yStepRatio,
	}
}

// ResetSwipeTargetState 清空缓存的滑动目标状态，供 Pipeline 在生命周期边界调用。
func ResetSwipeTargetState() {
	stateMu.Lock()
	defer stateMu.Unlock()
	lastSwipeTargetState = nil
}
