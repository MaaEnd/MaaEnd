package autostockpile

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
