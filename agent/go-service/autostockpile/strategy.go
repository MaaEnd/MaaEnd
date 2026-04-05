package autostockpile

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

// buildPriceLimitsForRegion 根据地区名称，通过 region_base + tier_base 公式计算各档位的价格阈值。
// 若 region 不在 regionBases 中，返回空 map。
func buildPriceLimitsForRegion(region string) PriceLimitConfig {
	regionBase, ok := regionBases[region]
	if !ok {
		return make(PriceLimitConfig)
	}

	priceLimits := make(PriceLimitConfig, len(tierBases))
	for tierSuffix, tierBase := range tierBases {
		priceLimits[region+tierSuffix] = regionBase + tierBase
	}
	return priceLimits
}

// buildSelectionConfig 根据地区名称构建商品选择配置，使用公式计算价格阈值。
func buildSelectionConfig(region string) SelectionConfig {
	priceLimits := buildPriceLimitsForRegion(region)
	fallbackThreshold := minPositiveThreshold(priceLimits)
	return SelectionConfig{
		PriceLimits:       priceLimits,
		FallbackThreshold: fallbackThreshold,
	}
}
