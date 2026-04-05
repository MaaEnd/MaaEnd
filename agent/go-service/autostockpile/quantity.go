package autostockpile

import (
	"strconv"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
)

type quantityMode string

const (
	quantityModeSkip                  quantityMode = "Skip"
	quantityModeSwipeMax              quantityMode = "SwipeMax"
	quantityModeSwipeSpecificQuantity quantityMode = "SwipeSpecificQuantity"
)

type quantityDecision struct {
	Mode              quantityMode
	Target            int
	MaxBuy            int
	ConstraintApplied bool
	Reason            string
}

func resolveQuantityDecision(selection SelectionResult, data RecognitionData, cfg SelectionConfig) quantityDecision {
	switch {
	case selection.CurrentPrice < selection.Threshold:
		return resolveThresholdQuantityDecision(data.Quota.Current)
	case data.Quota.Overflow > 0:
		return resolveOverflowQuantityDecision(data.Quota)
	default:
		return resolveThresholdQuantityDecision(data.Quota.Current)
	}
}

func resolveThresholdQuantityDecision(quotaCurrent int) quantityDecision {
	return quantityDecision{
		Mode:   quantityModeSwipeMax,
		Reason: i18n.T("autostockpile.qty_reserve_disabled"),
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

func formatQuantityText(decision quantityDecision) string {
	switch decision.Mode {
	case quantityModeSwipeMax:
		return i18n.T("autostockpile.quantity_all")
	case quantityModeSwipeSpecificQuantity:
		return strconv.Itoa(decision.Target)
	default:
		return decision.Reason
	}
}
