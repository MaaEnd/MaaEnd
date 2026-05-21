package pullcount

import (
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
	"github.com/rs/zerolog/log"
)

// --- Calculation And Display --- //

type resourceValues struct {
	ConvertedOriginiumOroberyl int
	Oroberyl                   int
}

type calculationResult struct {
	ReservedOriginium         int
	ReservedOriginiumOroberyl int
	UsableOriginiumOroberyl   int
	ResourcePulls             int
	CurrentOnlyPulls          int
	CarryToNextPulls          int
	NextOnlyPulls             int
	NextPoolShopPulls         int
	NextPoolSigninPulls       int
	CurrentPoolTotal          int
	NextPoolTotal             int
}

// calculatePullCount converts resources and classified vouchers into current and next-pool totals.
func calculatePullCount(values resourceValues, summary voucherSummary, param *actionParam) calculationResult {
	reservedOriginiumOroberyl := param.ReservedOriginium * param.OriginiumToOroberyl
	usableOriginiumOroberyl := values.ConvertedOriginiumOroberyl - reservedOriginiumOroberyl
	if usableOriginiumOroberyl < 0 {
		usableOriginiumOroberyl = 0
	}

	resourcePulls := (values.Oroberyl + usableOriginiumOroberyl) / param.OroberylPerPull
	currentPoolTotal := resourcePulls + summary.CurrentOnlyPulls + summary.CarryToNextPulls
	nextPoolTotal := resourcePulls + summary.CarryToNextPulls + summary.NextOnlyPulls + param.NextPoolShopPulls + param.NextPoolSigninPulls

	return calculationResult{
		ReservedOriginium:         param.ReservedOriginium,
		ReservedOriginiumOroberyl: reservedOriginiumOroberyl,
		UsableOriginiumOroberyl:   usableOriginiumOroberyl,
		ResourcePulls:             resourcePulls,
		CurrentOnlyPulls:          summary.CurrentOnlyPulls,
		CarryToNextPulls:          summary.CarryToNextPulls,
		NextOnlyPulls:             summary.NextOnlyPulls,
		NextPoolShopPulls:         param.NextPoolShopPulls,
		NextPoolSigninPulls:       param.NextPoolSigninPulls,
		CurrentPoolTotal:          currentPoolTotal,
		NextPoolTotal:             nextPoolTotal,
	}
}

// formatResultFocus builds the user-visible calculation summary.
func formatResultFocus(values resourceValues, result calculationResult) string {
	return i18n.T(
		"pullcount.result",
		result.ResourcePulls,
		result.CurrentOnlyPulls,
		result.CarryToNextPulls,
		result.NextOnlyPulls,
		result.NextPoolShopPulls,
		result.NextPoolSigninPulls,
		result.CurrentPoolTotal,
		result.NextPoolTotal,
		values.Oroberyl,
		values.ConvertedOriginiumOroberyl,
		result.ReservedOriginium,
		result.ReservedOriginiumOroberyl,
		result.UsableOriginiumOroberyl,
	)
}

// logCalculation writes structured details for troubleshooting pull-count results.
func logCalculation(session *runSession, summary voucherSummary, result calculationResult) {
	log.Info().
		Str("component", componentName).
		Int("oroberyl", session.Values.Oroberyl).
		Int("reserved_originium", result.ReservedOriginium).
		Int("converted_originium_oroberyl", session.Values.ConvertedOriginiumOroberyl).
		Int("reserved_originium_oroberyl", result.ReservedOriginiumOroberyl).
		Int("usable_converted_originium_oroberyl", result.UsableOriginiumOroberyl).
		Int("resource_pulls", result.ResourcePulls).
		Int("current_only_pulls", result.CurrentOnlyPulls).
		Int("carry_to_next_pulls", result.CarryToNextPulls).
		Int("next_only_pulls", result.NextOnlyPulls).
		Int("next_pool_shop_pulls", result.NextPoolShopPulls).
		Int("next_pool_signin_pulls", result.NextPoolSigninPulls).
		Int("current_pool_total", result.CurrentPoolTotal).
		Int("next_pool_total", result.NextPoolTotal).
		Interface("voucher_matches", summary.Matches).
		Msg("pull count calculated")
}
