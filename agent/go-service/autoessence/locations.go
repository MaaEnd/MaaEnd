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

// locationLabelKeys maps AutoEssence RegionNodes keys to the interface region-name
// i18n keys used by task options ($global.region.*).
var locationLabelKeys = map[string]string{
	"VFTheHub":                   "global.region.TheHub",
	"VFOriginiumSciencePark":     "global.region.OriginiumSciencePark",
	"VFOriginLodespring":         "global.region.OriginLodespring",
	"VFPowerPlateau":             "global.region.PowerPlateau",
	"WLWulingCity":               "global.region.WulingCity",
	"WLQingboStockade":           "global.region.QingboStockade",
	"WLMarkerStone":              "global.region.MarkerStone",
	"WLTestArea":                 "global.region.TestArea",
	"WLSwordVaultDale":           "global.region.SwordVaultDale",
	"WLYinglungPass":             "global.region.YinglungPass",
	"WLNorthWulingExclusionZone": "global.region.NorthWulingExclusionZone",
	"WLSnowyForest":              "global.region.SnowyForest",
}

// RegionLabelKey returns the interface i18n key of a location's region name.
func RegionLabelKey(locationKey string) (string, bool) {
	key, ok := locationLabelKeys[locationKey]
	return key, ok
}

// SetAnchorNode returns GotoTriggerPointSetAnchor_<key>.
func SetAnchorNode(locationKey string) string {
	return "GotoTriggerPointSetAnchor_" + locationKey
}
