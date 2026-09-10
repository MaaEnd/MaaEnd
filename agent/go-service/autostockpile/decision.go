package autostockpile

import "errors"

func computeDecision(data RecognitionData, cfg SelectionConfig, bypassThresholdFilter bool) (SelectionResult, quantityDecision, error) {
	selection, err := selectBestProduct(data, cfg, bypassThresholdFilter)
	if err != nil {
		return SelectionResult{}, quantityDecision{}, err
	}
	if !selection.Selected {
		return selection, quantityDecision{}, nil
	}

	decision := resolveQuantityDecision(selection, data)
	return selection, decision, nil
}

// mapComputeDecisionErrorToAbortReason 将 computeDecision 的失败原因映射为 abort 原因。
// 调用方保证 err 非 nil（两个调用点都在 if err != nil 内）。
func mapComputeDecisionErrorToAbortReason(err error) AbortReason {
	var thresholdErr *thresholdConfigError
	if errors.As(err, &thresholdErr) {
		return AbortReasonThresholdConfigInvalidFatal
	}

	return AbortReasonGoodsTierInvalidFatal
}
