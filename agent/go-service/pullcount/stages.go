package pullcount

import (
	"fmt"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/ims"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/iconqty"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/maafocus"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

func isIMSCurrency(itemID string) bool {
	return itemID == itemDiamond || itemID == itemOriginium
}

func quantityOf(recognized map[string]int, itemID string) int {
	if isIMSCurrency(itemID) {
		return ims.ItemQuantity(itemID)
	}
	if qty, ok := recognized[itemID]; ok {
		return qty
	}
	return ims.ItemQuantity(itemID)
}

func recognizeItems(ctx *maa.Context, params actionParam) (map[string]int, bool) {
	out := make(map[string]int, len(params.Items))
	if len(params.Items) == 0 {
		return out, true
	}

	img, err := cacheScreenImage(ctx)
	if err != nil {
		log.Error().
			Err(err).
			Str("component", componentName).
			Msg("failed to cache screen image")
		maafocus.Print(ctx, i18n.T("pullcount.error.recognition_failed", err.Error()))
		return nil, false
	}

	optional := resolveOptional(params.Optional)
	for _, itemID := range sortedItemIDs(params.Items) {
		if isIMSCurrency(itemID) {
			log.Error().
				Str("component", componentName).
				Str("item_id", itemID).
				Msg("diamond and originium must come from IMS, not on-screen record")
			maafocus.Print(ctx, i18n.T("pullcount.error.invalid_params"))
			return nil, false
		}
		node := params.Items[itemID]
		displayName := iconqty.ItemDisplayName(itemID)
		qty, hit, err := readQuantityFromNode(ctx, img, node)
		if err != nil {
			log.Warn().
				Err(err).
				Str("component", componentName).
				Str("item_id", itemID).
				Str("node", node).
				Msg("failed to read quantity")
			maafocus.Print(ctx, i18n.T("pullcount.error.recognition_failed", fmt.Sprintf("%s: %s", displayName, err.Error())))
			return nil, false
		}
		if !hit {
			if !optional {
				log.Warn().
					Str("component", componentName).
					Str("item_id", itemID).
					Str("node", node).
					Msg("required recognizer not hit")
				maafocus.Print(ctx, i18n.T("pullcount.error.recognition_failed", displayName))
				return nil, false
			}
			qty = 0
		}
		out[itemID] = qty
		log.Info().
			Str("component", componentName).
			Str("item_id", itemID).
			Str("item_name", displayName).
			Str("node", node).
			Int("quantity", qty).
			Bool("hit", hit).
			Bool("optional", optional).
			Msg("item quantity recognized")
		if hit {
			maafocus.Print(ctx, i18n.T("pullcount.resource_read_success", displayName, qty))
		}
	}
	return out, true
}

func handleCalculate(ctx *maa.Context, params actionParam) bool {
	if err := ims.EnsureHydrated(); err != nil {
		log.Error().
			Err(err).
			Str("component", componentName).
			Msg("failed to hydrate ims cache")
		maafocus.Print(ctx, i18n.T("pullcount.error.recognition_failed", err.Error()))
		return false
	}
	if !ims.HasData() {
		err := fmt.Errorf("ims has no originium/diamond data")
		log.Warn().
			Err(err).
			Str("component", componentName).
			Msg("cannot calculate pull count")
		maafocus.Print(ctx, i18n.T("pullcount.error.recognition_failed", err.Error()))
		return false
	}

	recognized, ok := recognizeItems(ctx, params)
	if !ok {
		return false
	}

	values := resourceValues{
		Originium: ims.ItemQuantity(itemOriginium),
		Oroberyl:  ims.ItemQuantity(itemDiamond),
	}
	summary := voucherSummary{
		CarryToNextPulls: sumVoucherPulls(func(id string) int {
			return quantityOf(recognized, id)
		}),
	}
	result := calculatePullCount(values, summary)
	maafocus.Print(ctx, formatResultFocus(values, result))
	logCalculation(recognized, values, summary, result)
	return true
}

func sumVoucherPulls(quantity func(string) int) int {
	total := 0
	for itemID, pulls := range voucherPulls {
		total += quantity(itemID) * pulls
	}
	return total
}
