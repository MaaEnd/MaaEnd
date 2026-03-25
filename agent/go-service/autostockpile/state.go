package autostockpile

import "sync"

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
)

func copyRecognitionData(src RecognitionData) RecognitionData {
	dst := src // copies scalar fields (Quota, Sunday, StockBillAmount, StockBillAvailable)
	dst.Goods = append([]GoodsItem(nil), src.Goods...)
	return dst
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
