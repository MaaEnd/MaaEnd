package essencefilter

import (
	"fmt"
	"strconv"
)

func setByKey(o *EssenceFilterOptions, k string, v bool) error {
	switch k {
	case "rarity6_weapon":
		o.Rarity6Weapon = v
	case "rarity5_weapon":
		o.Rarity5Weapon = v
	case "rarity4_weapon":
		o.Rarity4Weapon = v
	case "flawless_essence":
		o.FlawlessEssence = v
	case "pure_essence":
		o.PureEssence = v
	default:
		return fmt.Errorf("unknown option key: %s", k)
	}
	return nil
}

func rarityListToString(rarities []int) string {
	switch len(rarities) {
	case 1:
		return strconv.Itoa(rarities[0])
	case 2:
		return fmt.Sprintf("%d 和 %d", rarities[0], rarities[1])
	case 3:
		return fmt.Sprintf("%d， %d 和 %d", rarities[0], rarities[1], rarities[2])
	case 4:
		return fmt.Sprintf("%d， %d， %d 和 %d", rarities[0], rarities[1], rarities[2], rarities[3])
	default:
		return fmt.Sprintf("%d+", len(rarities))
	}
}

func ResetGlobalOptions() {
	gOpt = EssenceFilterOptions{}
}

func GetGlobalOptions() EssenceFilterOptions {
	return gOpt // 返回副本
}
