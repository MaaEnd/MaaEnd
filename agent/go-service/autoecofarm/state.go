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
	arg        *maa.CustomRecognitionArg
	xStepRatio float64
	yStepRatio float64
}

func cloneArg(arg *maa.CustomRecognitionArg) *maa.CustomRecognitionArg {
	if arg == nil {
		return nil
	}

	copied := *arg
	return &copied
}

func cloneState(state *swipeTargetState) *swipeTargetState {
	if state == nil {
		return nil
	}

	return &swipeTargetState{
		arg:        cloneArg(state.arg),
		xStepRatio: state.xStepRatio,
		yStepRatio: state.yStepRatio,
	}
}

// getLastState 返回最近一次缓存状态的快照。
func getLastState() *swipeTargetState {
	stateMu.RLock()
	defer stateMu.RUnlock()

	return cloneState(lastSwipeTargetState)
}

// getLastArg 返回最近一次的识别参数副本。
func getLastArg() *maa.CustomRecognitionArg {
	state := getLastState()
	if state == nil {
		return nil
	}

	return state.arg
}

// getLastRatios 返回最近一次的 X/Y StepRatio。
func getLastRatios() (float64, float64, bool) {
	state := getLastState()
	if state == nil {
		return 0, 0, false
	}

	return state.xStepRatio, state.yStepRatio, true
}

// setLastState 保存最新的识别参数和 StepRatio。
func setLastState(arg *maa.CustomRecognitionArg, xStepRatio, yStepRatio float64) {
	stateMu.Lock()
	defer stateMu.Unlock()

	if arg == nil {
		lastSwipeTargetState = nil
		return
	}

	lastSwipeTargetState = &swipeTargetState{
		arg:        cloneArg(arg),
		xStepRatio: xStepRatio,
		yStepRatio: yStepRatio,
	}
}
