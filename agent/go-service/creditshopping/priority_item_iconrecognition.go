package creditshopping

func priorityItemNodeName(level int) string {
	return "CreditShoppingPriority" + itoaLevel(level) + "Item"
}

func itoaLevel(level int) string {
	switch level {
	case 1:
		return "1"
	case 2:
		return "2"
	case 3:
		return "3"
	default:
		return "?"
	}
}

func priorityItemIconRecognitionOverride(caseIDs []string) map[string]any {
	recognitionIDs := creditShoppingRecognitionItemIDs(caseIDs)
	return map[string]any{
		"recognition":         "Custom",
		"custom_recognition":  "IconRecognition",
		"roi":                 creditTradeIconROIWin32,
		"index":               0,
		"custom_recognition_param": map[string]any{
			"grid_type":    "credit_trade",
			"item_ids":     recognitionIDs,
			"order_by":     "natural",
			"deduplicate":  true,
		},
	}
}
