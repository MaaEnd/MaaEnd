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
	OroberylPulls             int
	UsableOriginiumPulls      int
	ResourcePulls             int
	CarryToNextPulls          int
	NextPoolShopPulls         int
	NextPoolSigninPulls       int
	CurrentPoolTotal          int
	NextPoolTotal             int
}

// calculatePullCount converts resources and classified vouchers into current and next-pool totals.
func calculatePullCount(values resourceValues, summary voucherSummary) calculationResult {
	reservedOriginiumOroberyl := reservedOriginium * originiumToOroberyl
	usableOriginiumOroberyl := values.ConvertedOriginiumOroberyl - reservedOriginiumOroberyl
	if usableOriginiumOroberyl < 0 {
		usableOriginiumOroberyl = 0
	}

	resourcePulls := (values.Oroberyl + usableOriginiumOroberyl) / oroberylPerPull
	oroberylPulls := values.Oroberyl / oroberylPerPull
	usableOriginiumPulls := usableOriginiumOroberyl / oroberylPerPull
	currentPoolTotal := resourcePulls + summary.CarryToNextPulls
	nextPoolTotal := resourcePulls + summary.CarryToNextPulls + nextPoolShopPulls + nextPoolSigninPulls

	return calculationResult{
		ReservedOriginium:         reservedOriginium,
		ReservedOriginiumOroberyl: reservedOriginiumOroberyl,
		UsableOriginiumOroberyl:   usableOriginiumOroberyl,
		OroberylPulls:             oroberylPulls,
		UsableOriginiumPulls:      usableOriginiumPulls,
		ResourcePulls:             resourcePulls,
		CarryToNextPulls:          summary.CarryToNextPulls,
		NextPoolShopPulls:         nextPoolShopPulls,
		NextPoolSigninPulls:       nextPoolSigninPulls,
		CurrentPoolTotal:          currentPoolTotal,
		NextPoolTotal:             nextPoolTotal,
	}
}

// formatResultFocus builds the user-visible calculation summary.
func formatResultFocus(values resourceValues, result calculationResult) string {
	return i18n.T(
		"pullcount.result",
		result.ResourcePulls,
		values.Oroberyl,
		result.OroberylPulls,
		values.ConvertedOriginiumOroberyl,
		result.ReservedOriginium,
		result.ReservedOriginiumOroberyl,
		result.UsableOriginiumOroberyl,
		result.UsableOriginiumPulls,
		result.CarryToNextPulls,
		result.NextPoolShopPulls,
		result.NextPoolSigninPulls,
		result.CurrentPoolTotal,
		result.NextPoolTotal,
	)
}

// logCalculation writes structured details for troubleshooting pull-count results.
func logCalculation(session *runSession, result calculationResult) {
	log.Info().
		Str("component", componentName).
		Interface("values", session.Values).
		Interface("summary", session.Vouchers).
		Interface("result", result).
		Int("recorded_voucher_hits", len(session.VoucherHits)).
		Msg("pull count calculated")
}
