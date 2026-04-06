package autostockpile

import "github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"

type quantityMode string

const (
	quantityModeSkip                  quantityMode = "Skip"
	quantityModeSwipeMax              quantityMode = "SwipeMax"
	quantityModeSwipeSpecificQuantity quantityMode = "SwipeSpecificQuantity"
)

type quantityDecision struct {
	Mode   quantityMode
	Target int
	Reason string
}

func resolveQuantityDecision(selection SelectionResult, data RecognitionData) quantityDecision {
	switch {
	case selection.CurrentPrice < selection.Threshold:
		return resolveThresholdQuantityDecision()
	case data.Quota.Overflow > 0:
		return resolveOverflowQuantityDecision(data.Quota)
	default:
		return resolveThresholdQuantityDecision()
	}
}

func resolveThresholdQuantityDecision() quantityDecision {
	return quantityDecision{
		Mode:   quantityModeSwipeMax,
		Reason: i18n.T("autostockpile.qty_below_threshold_buy"),
	}
}

func resolveOverflowQuantityDecision(quota QuotaInfo) quantityDecision {
	overflowTarget := quota.Overflow
	if overflowTarget > quota.Current {
		overflowTarget = quota.Current
	}

	if overflowTarget <= 0 {
		return quantityDecision{
			Mode:   quantityModeSkip,
			Reason: i18n.T("autostockpile.qty_overflow_invalid"),
		}
	}

	return quantityDecision{
		Mode:   quantityModeSwipeSpecificQuantity,
		Target: overflowTarget,
		Reason: i18n.T("autostockpile.qty_overflow_buy"),
	}
}
