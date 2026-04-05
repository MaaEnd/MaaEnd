package autostockpile

func computeDecision(data RecognitionData, cfg SelectionConfig, bypassThresholdFilter bool) (SelectionResult, quantityDecision) {
	selection := SelectBestProduct(data, cfg, bypassThresholdFilter)
	if !selection.Selected {
		return selection, quantityDecision{}
	}

	decision := resolveQuantityDecision(selection, data)
	return selection, decision
}
