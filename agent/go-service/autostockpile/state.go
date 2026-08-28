package autostockpile

import (
	"sync"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

type currentDecision struct {
	Selection        SelectionResult
	QuantityDecision quantityDecision
}

// DecisionState holds the shared state produced by the recognition phase
// and consumed by the selection/action phase.
type DecisionState struct {
	Region             string
	EffectiveConfig    SelectionConfig
	RawRecognitionData RecognitionData
	CurrentDecision    currentDecision
}

var (
	stateMu       sync.Mutex
	decisionState *DecisionState

	// minBuyRegion 是任务内缓存，记录本次 AutoStockpile 运行中
	// 第一个执行「至少购买一个」决策的地区。启用该功能时只有
	// 该地区允许在正常选品失败后降级购买，避免跨地区重复强迫购买。
	minBuyRegion string
)

func copyRecognitionData(src RecognitionData) RecognitionData {
	dst := src
	dst.Goods = append([]GoodsItem(nil), src.Goods...)
	dst.SecondPageOnlyIDs = append([]string(nil), src.SecondPageOnlyIDs...)
	return dst
}

func isSecondPageOnlyID(data RecognitionData, productID string) bool {
	for _, id := range data.SecondPageOnlyIDs {
		if id == productID {
			return true
		}
	}
	return false
}

// getDecisionState returns nil if no state has been set; otherwise returns a deep copy.
func getDecisionState() *DecisionState {
	stateMu.Lock()
	defer stateMu.Unlock()
	if decisionState == nil {
		return nil
	}
	copied := *decisionState
	copied.RawRecognitionData = copyRecognitionData(decisionState.RawRecognitionData)
	return &copied
}

// setDecisionState stores a deep copy of s. A nil input clears the state.
func setDecisionState(s *DecisionState) {
	stateMu.Lock()
	defer stateMu.Unlock()
	if s == nil {
		decisionState = nil
		return
	}
	copied := *s
	copied.RawRecognitionData = copyRecognitionData(s.RawRecognitionData)
	decisionState = &copied
}

// getMinBuyRegion returns the task-level MinBuyRegion cache.
func getMinBuyRegion() string {
	stateMu.Lock()
	defer stateMu.Unlock()
	return minBuyRegion
}

// setMinBuyRegion stores the task-level MinBuyRegion cache.
func setMinBuyRegion(region string) {
	stateMu.Lock()
	defer stateMu.Unlock()
	minBuyRegion = region
}

// resetMinBuyRegion clears the task-level MinBuyRegion cache.
func resetMinBuyRegion() {
	stateMu.Lock()
	defer stateMu.Unlock()
	minBuyRegion = ""
}

var _ maa.TaskerEventSink = &taskStateResetSink{}

// taskStateResetSink 在 AutoStockpile 任务开始时清理任务内缓存，
// 保证每次运行都从空缓存重新决定「至少购买一个」地区。
type taskStateResetSink struct{}

// OnTaskerTask handles task lifecycle events.
func (s *taskStateResetSink) OnTaskerTask(_ *maa.Tasker, event maa.EventStatus, detail maa.TaskerTaskDetail) {
	if event != maa.EventStatusStarting || detail.Entry != autoStockpileMainEntryNodeName {
		return
	}

	resetMinBuyRegion()

	log.Info().
		Str("component", autoStockpileComponent).
		Uint64("task_id", detail.TaskID).
		Msg("AutoStockpile task started, min buy region cache reset")
}
