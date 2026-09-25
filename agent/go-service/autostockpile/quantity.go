package autostockpile

import "github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"

type quantityMode string

const (
	quantityModeSwipeMax              quantityMode = "SwipeMax"
	quantityModeSwipeSpecificQuantity quantityMode = "SwipeSpecificQuantity"
)

type quantityDecision struct {
	Mode   quantityMode
	Target int
	Reason string
}

// resolveQuantityDecision 依据是否触发防溢出决定购买数量策略。
// 选中商品价格低于阈值时始终买满；价格不低于阈值只可能出现在溢出放行路径，按防溢出数量购买。
func resolveQuantityDecision(selection SelectionResult, data RecognitionData) quantityDecision {
	if selection.CurrentPrice < selection.Threshold {
		return resolveThresholdQuantityDecision()
	}
	return resolveOverflowQuantityDecision(data.Quota)
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

	return quantityDecision{
		Mode:   quantityModeSwipeSpecificQuantity,
		Target: overflowTarget,
		Reason: i18n.T("autostockpile.qty_overflow_buy"),
	}
}
