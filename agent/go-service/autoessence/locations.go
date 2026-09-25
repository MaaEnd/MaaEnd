package autoessence

// locationNameToKey maps locations.json display names to AutoEssence RegionNodes keys.
var locationNameToKey = map[string]string{
	"重度能量淤积点·枢纽区":   "VFTheHub",
	"重度能量淤积点·源石研究园": "VFOriginiumSciencePark",
	"重度能量淤积点·矿脉源区":  "VFOriginLodespring",
	"重度能量淤积点·供能高地":  "VFPowerPlateau",
	"重度能量淤积点·武陵城":   "WLWulingCity",
	"重度能量淤积点·清波寨":   "WLQingboStockade",
	"重度能量淤积点·首墩":    "WLMarkerStone",
	"重度能量淤积点·试验园区":  "WLTestArea",
	"重度能量淤积点·藏剑谷":   "WLSwordVaultDale",
	"重度能量淤积点·应龙关":   "WLYinglungPass",
	"重度能量淤积点·北部禁区":  "WLNorthWulingExclusionZone",
	"重度能量淤积点·雪松林":   "WLSnowyForest",
}

// LocationKeyByName returns the pipeline location key for a locations.json name.
func LocationKeyByName(name string) (string, bool) {
	key, ok := locationNameToKey[name]
	return key, ok
}

// SetAnchorNode returns GotoTriggerPointSetAnchor_<key>.
func SetAnchorNode(locationKey string) string {
	return "GotoTriggerPointSetAnchor_" + locationKey
}
