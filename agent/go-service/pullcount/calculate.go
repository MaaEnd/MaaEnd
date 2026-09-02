package pullcount

import (
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
	"github.com/rs/zerolog/log"
)

type resourceValues struct {
	Originium int
	Oroberyl  int
}

type voucherSummary struct {
	CarryToNextPulls int
}

type calculationResult struct {
	ReservedOriginium          int
	ReservedOriginiumOroberyl  int
	ConvertedOriginiumOroberyl int
	UsableOriginiumOroberyl    int
	OroberylPulls              int
	UsableOriginiumPulls       int
	ResourcePulls              int
	CarryToNextPulls           int
	NextPoolShopPulls          int
	NextPoolSigninPulls        int
	CurrentPoolTotal           int
	NextPoolTotal              int
}

func (v resourceValues) convertedOriginiumOroberyl() int {
	return v.Originium * originiumToOroberyl
}

// calculatePullCount converts raw originium, oroberyl, and classified vouchers
// into current and next-pool totals. Originium is the raw 衍质源石 count.
func calculatePullCount(values resourceValues, summary voucherSummary) calculationResult {
	converted := values.convertedOriginiumOroberyl()
	reservedOriginiumOroberyl := reservedOriginium * originiumToOroberyl
	usableOriginiumOroberyl := converted - reservedOriginiumOroberyl
	if usableOriginiumOroberyl < 0 {
		usableOriginiumOroberyl = 0
	}

	resourcePulls := (values.Oroberyl + usableOriginiumOroberyl) / oroberylPerPull
	oroberylPulls := values.Oroberyl / oroberylPerPull
	usableOriginiumPulls := usableOriginiumOroberyl / oroberylPerPull
	currentPoolTotal := resourcePulls + summary.CarryToNextPulls
	nextPoolTotal := resourcePulls + summary.CarryToNextPulls + nextPoolShopPulls + nextPoolSigninPulls

	return calculationResult{
		ReservedOriginium:          reservedOriginium,
		ReservedOriginiumOroberyl:  reservedOriginiumOroberyl,
		ConvertedOriginiumOroberyl: converted,
		UsableOriginiumOroberyl:    usableOriginiumOroberyl,
		OroberylPulls:              oroberylPulls,
		UsableOriginiumPulls:       usableOriginiumPulls,
		ResourcePulls:              resourcePulls,
		CarryToNextPulls:           summary.CarryToNextPulls,
		NextPoolShopPulls:          nextPoolShopPulls,
		NextPoolSigninPulls:        nextPoolSigninPulls,
		CurrentPoolTotal:           currentPoolTotal,
		NextPoolTotal:              nextPoolTotal,
	}
}

func formatResultFocus(values resourceValues, result calculationResult) string {
	return i18n.T(
		"pullcount.result",
		result.ResourcePulls,
		values.Oroberyl,
		result.OroberylPulls,
		result.ConvertedOriginiumOroberyl,
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

func logCalculation(recognized map[string]int, values resourceValues, summary voucherSummary, result calculationResult) {
	log.Info().
		Str("component", componentName).
		Interface("recognized", recognized).
		Interface("values", values).
		Interface("summary", summary).
		Interface("result", result).
		Msg("pull count calculated")
}
