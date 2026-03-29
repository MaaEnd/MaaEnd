package autostockpile

import (
	"strconv"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
	"github.com/rs/zerolog/log"
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

func resolveQuantityDecision(selection SelectionResult, data RecognitionData, cfg SelectionConfig) (quantityDecision, error) {
	upperBound, err := resolveQuantityUpperBound(data.StockBillAvailable, data.StockBillAmount, cfg.ReserveStockBill, selection.CurrentPrice, data.Quota.Current)
	if err != nil {
		log.Error().
			Err(err).
			Str("component", autoStockpileComponent).
			Bool("stock_bill_available", data.StockBillAvailable).
			Int("stock_bill_amount", data.StockBillAmount).
			Int("reserve_stock_bill", cfg.ReserveStockBill).
			Int("price", selection.CurrentPrice).
			Msg("failed to resolve quantity decision")
		return quantityDecision{}, err
	}

	switch {
	case selection.CurrentPrice < selection.Threshold:
		return resolveThresholdQuantityDecision(upperBound, data.Quota.Current), nil
	case cfg.SundayMode && data.Sunday:
		return resolveThresholdQuantityDecision(upperBound, data.Quota.Current), nil
	case cfg.OverflowMode && data.Quota.Overflow > 0:
		return resolveOverflowQuantityDecision(upperBound, data.Quota), nil
	default:
		return resolveThresholdQuantityDecision(upperBound, data.Quota.Current), nil
	}
}

func resolveThresholdQuantityDecision(upperBound quantityUpperBound, quotaCurrent int) quantityDecision {
	if !upperBound.ConstraintApplied {
		return quantityDecision{
			Mode:              quantityModeSwipeMax,
			MaxBuy:            upperBound.MaxBuy,
			ConstraintApplied: upperBound.ConstraintApplied,
			Reason:            i18n.T("autostockpile.qty_reserve_disabled"),
		}
	}

	if upperBound.CappedQuantity <= 0 {
		return quantityDecision{
			Mode:              quantityModeSkip,
			MaxBuy:            upperBound.MaxBuy,
			ConstraintApplied: upperBound.ConstraintApplied,
			Reason:            i18n.T("autostockpile.qty_reserve_zero"),
		}
	}

	if upperBound.CappedQuantity == quotaCurrent {
		return quantityDecision{
			Mode:              quantityModeSwipeMax,
			MaxBuy:            upperBound.MaxBuy,
			ConstraintApplied: upperBound.ConstraintApplied,
			Reason:            i18n.T("autostockpile.qty_reserve_allows_all"),
		}
	}

	return quantityDecision{
		Mode:              quantityModeSwipeSpecificQuantity,
		Target:            upperBound.CappedQuantity,
		MaxBuy:            upperBound.MaxBuy,
		ConstraintApplied: upperBound.ConstraintApplied,
		Reason:            i18n.T("autostockpile.qty_reserve_limited"),
	}
}

func resolveOverflowQuantityDecision(upperBound quantityUpperBound, quota QuotaInfo) quantityDecision {
	overflowTarget := quota.Overflow
	if overflowTarget > quota.Current {
		overflowTarget = quota.Current
	}

	if !upperBound.ConstraintApplied {
		if overflowTarget <= 0 {
			return quantityDecision{
				Mode:              quantityModeSkip,
				MaxBuy:            upperBound.MaxBuy,
				ConstraintApplied: upperBound.ConstraintApplied,
				Reason:            i18n.T("autostockpile.qty_overflow_invalid"),
			}
		}

		return quantityDecision{
			Mode:              quantityModeSwipeSpecificQuantity,
			Target:            overflowTarget,
			MaxBuy:            upperBound.MaxBuy,
			ConstraintApplied: upperBound.ConstraintApplied,
			Reason:            i18n.T("autostockpile.qty_overflow_buy"),
		}
	}

	target := min(overflowTarget, upperBound.CappedQuantity)
	if target <= 0 {
		return quantityDecision{
			Mode:              quantityModeSkip,
			MaxBuy:            upperBound.MaxBuy,
			ConstraintApplied: upperBound.ConstraintApplied,
			Reason:            i18n.T("autostockpile.qty_overflow_reserve_zero"),
		}
	}

	reason := i18n.T("autostockpile.qty_overflow_buy")
	if target < overflowTarget {
		reason = i18n.T("autostockpile.qty_overflow_reserve_limited")
	}

	return quantityDecision{
		Mode:              quantityModeSwipeSpecificQuantity,
		Target:            target,
		MaxBuy:            upperBound.MaxBuy,
		ConstraintApplied: upperBound.ConstraintApplied,
		Reason:            reason,
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
