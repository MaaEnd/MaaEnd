package autostockpile

import (
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
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

func buildSelectionPipelineOverride(ctx *maa.Context, selection SelectionResult, decision quantityDecision) (map[string]any, error) {
	override := map[string]any{
		selectedGoodsClickNodeName: map[string]any{
			"enabled":  true,
			"template": []string{BuildTemplatePath(selection.ProductID)},
		},
		swipeMaxNodeName: map[string]any{
			"enabled": decision.Mode == quantityModeSwipeMax,
		},
	}

	if decision.Mode != quantityModeSwipeSpecificQuantity {
		override[swipeSpecificQuantityNodeName] = map[string]any{
			"enabled": false,
		}
		return override, nil
	}

	customActionParam, err := loadSwipeSpecificQuantityCustomActionParam(ctx)
	if err != nil {
		return nil, err
	}

	override[swipeSpecificQuantityNodeName] = buildSwipeSpecificQuantityOverride(customActionParam, decision.Target)
	return override, nil
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

func loadSwipeSpecificQuantityCustomActionParam(ctx *maa.Context) (map[string]any, error) {
	if ctx == nil {
		return nil, fmt.Errorf("context is nil")
	}

	node, err := ctx.GetNode(swipeSpecificQuantityNodeName)
	if err != nil {
		return nil, err
	}

	if node.Action == nil {
		return nil, fmt.Errorf("node %s missing action", swipeSpecificQuantityNodeName)
	}

	param, ok := node.Action.Param.(*maa.CustomActionParam)
	if !ok || param == nil {
		return nil, fmt.Errorf("node %s action param type %T is not *maa.CustomActionParam", swipeSpecificQuantityNodeName, node.Action.Param)
	}

	return normalizeCustomActionParam(param.CustomActionParam)
}

func buildSwipeSpecificQuantityOverride(customActionParam map[string]any, target int) map[string]any {
	clonedParam := make(map[string]any, len(customActionParam))
	for key, item := range customActionParam {
		clonedParam[key] = item
	}
	clonedParam["Target"] = target

	return map[string]any{
		"enabled": true,
		"action": map[string]any{
			"param": map[string]any{
				"custom_action_param": clonedParam,
			},
		},
	}
}

func normalizeCustomActionParam(raw any) (map[string]any, error) {
	switch value := raw.(type) {
	case map[string]any:
		cloned := make(map[string]any, len(value))
		for key, item := range value {
			cloned[key] = item
		}
		return cloned, nil
	case string:
		var nested any
		if err := json.Unmarshal([]byte(value), &nested); err != nil {
			return nil, err
		}
		return normalizeCustomActionParam(nested)
	default:
		return nil, fmt.Errorf("unsupported custom_action_param type %T", raw)
	}
}
