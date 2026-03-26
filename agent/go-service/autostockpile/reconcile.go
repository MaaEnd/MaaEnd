package autostockpile

import (
	"strconv"
	"strings"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/maafocus"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

type ReconcileDecisionAction struct{}

var _ maa.CustomActionRunner = &ReconcileDecisionAction{}

func enableDecisionReadyOverride(ctx *maa.Context) error {
	return ctx.OverridePipeline(map[string]any{
		"AutoStockpileRelayNodeDecisionReady": map[string]any{
			"enabled": true,
		},
	})
}

func (a *ReconcileDecisionAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if arg == nil {
		log.Error().
			Str("component", "autostockpile").
			Msg("custom action arg is nil")
		return false
	}

	if arg.RecognitionDetail == nil {
		log.Error().
			Str("component", "autostockpile").
			Msg("recognition detail is nil")
		return false
	}

	state := getDecisionState()
	if state == nil {
		log.Error().
			Str("component", "autostockpile").
			Msg("decision state is nil")
		return false
	}

	ocrTexts := extractOCRTexts(arg.RecognitionDetail)
	if len(ocrTexts) == 0 {
		log.Error().
			Str("component", "autostockpile").
			Msg("reconcile recognition detail contains no ocr text")
		return false
	}

	priceText := ""
	for _, text := range ocrTexts {
		if match := priceRe.FindStringSubmatch(text); len(match) == 2 {
			priceText = match[1]
			break
		}
	}
	if priceText == "" {
		log.Error().
			Str("component", "autostockpile").
			Str("ocr_texts", strings.Join(ocrTexts, ",")).
			Msg("failed to extract reconcile price text from recognition detail")
		return false
	}

	price, err := strconv.Atoi(strings.TrimSpace(priceText))
	if err != nil {
		log.Error().
			Err(err).
			Str("component", "autostockpile").
			Str("ocr_text", priceText).
			Msg("failed to parse reconcile price")
		return false
	}

	oldPrice := state.CurrentDecision.Selection.CurrentPrice
	priceChanged := price != oldPrice
	if !priceChanged {
		if err := enableDecisionReadyOverride(ctx); err != nil {
			log.Error().
				Err(err).
				Str("component", "autostockpile").
				Str("node", "AutoStockpileRelayNodeDecisionReady").
				Msg("failed to override proceed pipeline when price unchanged")
			return false
		}

		log.Info().
			Str("component", "autostockpile").
			Str("product_id", state.CurrentDecision.Selection.ProductID).
			Int("price", price).
			Msg("reconcile price unchanged, proceed path selected")
		return true
	}

	updatedData := copyRecognitionData(state.RawRecognitionData)
	matched := false
	for i := range updatedData.Goods {
		if updatedData.Goods[i].ID != state.CurrentDecision.Selection.ProductID {
			continue
		}
		updatedData.Goods[i].Price = price
		matched = true
		break
	}

	if !matched {
		log.Warn().
			Str("component", "autostockpile").
			Str("product_id", state.CurrentDecision.Selection.ProductID).
			Msg("target product not found in goods list during reconcile")
	}

	bypassThresholdFilter := (updatedData.Quota.Overflow > 0 && state.EffectiveConfig.OverflowMode) ||
		(updatedData.Sunday && state.EffectiveConfig.SundayMode)

	newSelection, newQuantityDecision, err := computeDecision(updatedData, state.EffectiveConfig, bypassThresholdFilter)
	if err != nil {
		log.Error().
			Err(err).
			Str("component", "autostockpile").
			Msg("failed to recompute decision during reconcile")
		return false
	}

	if priceChanged {
		maafocus.Print(ctx, i18n.T("autostockpile.reconcile_price_corrected", oldPrice, price))
	}

	isEquivalent := newSelection.Selected &&
		newSelection.ProductID == state.CurrentDecision.Selection.ProductID &&
		newQuantityDecision.Mode == state.CurrentDecision.QuantityDecision.Mode &&
		(newQuantityDecision.Mode != quantityModeSwipeSpecificQuantity ||
			newQuantityDecision.Target == state.CurrentDecision.QuantityDecision.Target)

	if isEquivalent {
		if err := enableDecisionReadyOverride(ctx); err != nil {
			log.Error().
				Err(err).
				Str("component", "autostockpile").
				Str("node", "AutoStockpileRelayNodeDecisionReady").
				Msg("failed to override proceed pipeline")
			return false
		}

		setDecisionState(&DecisionState{
			Region:             state.Region,
			EffectiveConfig:    state.EffectiveConfig,
			RawRecognitionData: updatedData,
			CurrentDecision: currentDecision{
				Selection:        newSelection,
				QuantityDecision: newQuantityDecision,
			},
		})

		maafocus.Print(ctx, i18n.T("autostockpile.reconcile_decision_unchanged"))

		log.Info().
			Str("component", "autostockpile").
			Str("product_id", state.CurrentDecision.Selection.ProductID).
			Int("price", price).
			Msg("reconcile decision unchanged, proceed path selected")
		return true
	}

	if !newSelection.Selected {
		override := buildSkipResetOverride()
		if err := ctx.OverridePipeline(override); err != nil {
			log.Error().
				Err(err).
				Str("component", "autostockpile").
				Str("node", selectedGoodsClickNodeName+","+swipeMaxNodeName+","+swipeSpecificQuantityNodeName).
				Msg("failed to override retry skip-reset pipeline")
			return false
		}

		setDecisionState(&DecisionState{
			Region:             state.Region,
			EffectiveConfig:    state.EffectiveConfig,
			RawRecognitionData: updatedData,
			CurrentDecision:    currentDecision{},
		})

		maafocus.Print(ctx, i18n.T("autostockpile.no_qualifying_product", newSelection.Reason))

		log.Info().
			Str("component", "autostockpile").
			Str("product_id", state.CurrentDecision.Selection.ProductID).
			Str("reason", newSelection.Reason).
			Int("price", price).
			Msg("reconcile found no qualifying product, retry path selected")
		return true
	}

	override, err := buildSelectionPipelineOverride(ctx, newSelection, newQuantityDecision)
	if err != nil {
		log.Error().
			Err(err).
			Str("component", "autostockpile").
			Msg("failed to build retry selection pipeline override")
		return false
	}

	if err := ctx.OverridePipeline(override); err != nil {
		log.Error().
			Err(err).
			Str("component", "autostockpile").
			Str("node", selectedGoodsClickNodeName+","+swipeMaxNodeName+","+swipeSpecificQuantityNodeName).
			Msg("failed to override retry selection pipeline")
		return false
	}

	setDecisionState(&DecisionState{
		Region:             state.Region,
		EffectiveConfig:    state.EffectiveConfig,
		RawRecognitionData: updatedData,
		CurrentDecision: currentDecision{
			Selection:        newSelection,
			QuantityDecision: newQuantityDecision,
		},
	})

	maafocus.Print(ctx, i18n.T("autostockpile.product_selected", formatSelectionMode(newSelection, updatedData, state.EffectiveConfig), newSelection.ProductName, newSelection.CurrentPrice, newSelection.Threshold, formatQuantityText(newQuantityDecision)))

	log.Info().
		Str("component", "autostockpile").
		Str("old_product_id", state.CurrentDecision.Selection.ProductID).
		Str("new_product_id", newSelection.ProductID).
		Str("new_quantity_mode", string(newQuantityDecision.Mode)).
		Int("new_quantity_target", newQuantityDecision.Target).
		Int("price", price).
		Msg("reconcile decision changed, retry path selected")

	return true
}
