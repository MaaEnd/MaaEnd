package pullcount

import "github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"

type resourceStock struct {
	Originium        int
	Oroberyl         int
	CarryToNextPulls int
}

type calculationResult struct {
	ReserveOriginium        int
	ReserveOroberyl         int
	UsableOriginium         int
	UsableOroberyl          int
	UsableOriginiumOroberyl int
	OroberylPulls           int
	UsableOriginiumPulls    int
	ResourcePulls           int
	CarryToNextPulls        int
	NextPoolShopPulls       int
	NextPoolSigninPulls     int
	CurrentPoolTotal        int
	NextPoolTotal           int
}

// calculatePullCount converts IMS stock and user reserves into current / next-pool totals.
func calculatePullCount(stock resourceStock, reserveOriginium, reserveOroberyl int) calculationResult {
	usableOriginium := stock.Originium - reserveOriginium
	if usableOriginium < 0 {
		usableOriginium = 0
	}
	usableOroberyl := stock.Oroberyl - reserveOroberyl
	if usableOroberyl < 0 {
		usableOroberyl = 0
	}

	usableOriginiumOroberyl := usableOriginium * originiumToOroberyl
	resourcePulls := (usableOroberyl + usableOriginiumOroberyl) / oroberylPerPull
	oroberylPulls := usableOroberyl / oroberylPerPull
	usableOriginiumPulls := usableOriginiumOroberyl / oroberylPerPull
	currentPoolTotal := resourcePulls + stock.CarryToNextPulls
	nextPoolTotal := currentPoolTotal + nextPoolShopPulls + nextPoolSigninPulls

	return calculationResult{
		ReserveOriginium:        reserveOriginium,
		ReserveOroberyl:         reserveOroberyl,
		UsableOriginium:         usableOriginium,
		UsableOroberyl:          usableOroberyl,
		UsableOriginiumOroberyl: usableOriginiumOroberyl,
		OroberylPulls:           oroberylPulls,
		UsableOriginiumPulls:    usableOriginiumPulls,
		ResourcePulls:           resourcePulls,
		CarryToNextPulls:        stock.CarryToNextPulls,
		NextPoolShopPulls:       nextPoolShopPulls,
		NextPoolSigninPulls:     nextPoolSigninPulls,
		CurrentPoolTotal:        currentPoolTotal,
		NextPoolTotal:           nextPoolTotal,
	}
}

// formatResultFocus builds the user-visible calculation summary.
func formatResultFocus(stock resourceStock, result calculationResult) string {
	return i18n.T(
		"pullcount.result",
		result.ResourcePulls,
		stock.Oroberyl,
		result.ReserveOroberyl,
		result.UsableOroberyl,
		result.OroberylPulls,
		stock.Originium,
		result.ReserveOriginium,
		result.UsableOriginiumOroberyl,
		result.UsableOriginiumPulls,
		result.CarryToNextPulls,
		result.NextPoolShopPulls,
		result.NextPoolSigninPulls,
		result.CurrentPoolTotal,
		result.NextPoolTotal,
	)
}
