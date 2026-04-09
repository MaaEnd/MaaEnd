package autostockpile

import "errors"

func computeDecision(data RecognitionData, cfg SelectionConfig, bypassThresholdFilter bool) (SelectionResult, quantityDecision, error) {
	selection, err := SelectBestProduct(data, cfg, bypassThresholdFilter)
	if err != nil {
		return SelectionResult{}, quantityDecision{}, err
	}
	if !selection.Selected {
		return selection, quantityDecision{}, nil
	}

	decision := resolveQuantityDecision(selection, data)
	return selection, decision, nil
}

func mapComputeDecisionErrorToAbortReason(err error) AbortReason {
	if err == nil {
		return AbortReasonNone
	}

	var thresholdErr *thresholdConfigError
	if errors.As(err, &thresholdErr) {
		return AbortReasonThresholdConfigInvalidFatal
	}

	return AbortReasonGoodsTierInvalidFatal
}
