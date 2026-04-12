package autostockpile

import (
	"fmt"
	"time"
)

const (
	regionBaseValleyIV = 0
	regionBaseWuling   = 600
)

const (
	tierBaseTier1 = 600
	tierBaseTier2 = 900
	tierBaseTier3 = 1200
)

var regionBases = map[string]int{
	"ValleyIV": regionBaseValleyIV,
	"Wuling":   regionBaseWuling,
}

var tierBases = map[string]int{
	"Tier1": tierBaseTier1,
	"Tier2": tierBaseTier2,
	"Tier3": tierBaseTier3,
}

var weekdayAdjustments = map[time.Weekday]int{
	time.Monday:    -50,
	time.Tuesday:   0,
	time.Wednesday: -150,
	time.Thursday:  -200,
	time.Friday:    -250,
	time.Saturday:  -200,
	time.Sunday:    -50,
}

// buildPriceLimitsForRegion 根据地区名称构建各档位的价格阈值。
func buildPriceLimitsForRegion(region string, weekday time.Weekday, applyWeekdayAdjustment bool) (PriceLimitConfig, error) {
	regionBase, ok := regionBases[region]
	if !ok {
		return nil, fmt.Errorf("region %q is not configured", region)
	}

	weekdayAdjustment := 0
	if applyWeekdayAdjustment {
		var ok bool
		weekdayAdjustment, ok = weekdayAdjustments[weekday]
		if !ok {
			return nil, fmt.Errorf("weekday %d is not supported", weekday)
		}
	}

	priceLimits := make(PriceLimitConfig, len(tierBases))
	for tierSuffix, tierBase := range tierBases {
		priceLimits[region+"."+tierSuffix] = regionBase + tierBase + weekdayAdjustment
	}
	return priceLimits, nil
}

// buildSelectionConfig 根据地区名称构建商品选择配置，使用公式计算价格阈值。
func buildSelectionConfig(region string, loc *time.Location, applyWeekdayAdjustment bool) (SelectionConfig, error) {
	return buildSelectionConfigForWeekday(region, resolveServerWeekday(time.Now(), loc), applyWeekdayAdjustment)
}

func buildSelectionConfigForWeekday(region string, weekday time.Weekday, applyWeekdayAdjustment bool) (SelectionConfig, error) {
	priceLimits, err := buildPriceLimitsForRegion(region, weekday, applyWeekdayAdjustment)
	if err != nil {
		return SelectionConfig{}, err
	}

	return SelectionConfig{
		PriceLimits: priceLimits,
	}, nil
}
