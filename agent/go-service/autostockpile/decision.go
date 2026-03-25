package autostockpile

func computeDecision(data RecognitionData, cfg SelectionConfig, bypassThresholdFilter bool) (SelectionResult, quantityDecision, error) {
	selection := SelectBestProduct(data, cfg, bypassThresholdFilter)
	if !selection.Selected {
		return selection, quantityDecision{}, nil
	}

	decision, err := resolveQuantityDecision(selection, data, cfg)
	if err != nil {
		return SelectionResult{}, quantityDecision{}, err
	}

	return selection, decision, nil
}
