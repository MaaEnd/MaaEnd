package autoessence

// locationLevelToKey maps locations.json level_id to AutoEssence RegionNodes keys.
//
// 用 level_id（WorldEnergyPointTable 的 levelId）而不是显示名：levelId 与地区一一对应，不会随中文文案
// 或翻译改动而变化；显示名只在报错信息里用。
var locationLevelToKey = map[string]string{
	"map01_lv001": "VFTheHub",
	"map01_lv005": "VFOriginiumSciencePark",
	"map01_lv006": "VFOriginLodespring",
	"map01_lv007": "VFPowerPlateau",
	"map02_lv002": "WLWulingCity",
	"map02_lv003": "WLQingboStockade",
	"map02_lv004": "WLMarkerStone",
	"map02_lv005": "WLTestArea",
	"map02_lv006": "WLSwordVaultDale",
	"map02_lv007": "WLYinglungPass",
	"map02_lv008": "WLNorthWulingExclusionZone",
	"map02_lv009": "WLSnowyForest",
	"map02_lv010": "WLSisPainting",
}

// LocationKeyByLevel returns the pipeline location key for a locations.json level_id.
func LocationKeyByLevel(levelID string) (string, bool) {
	key, ok := locationLevelToKey[levelID]
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
	"WLSisPainting":              "global.region.SisPainting",
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
