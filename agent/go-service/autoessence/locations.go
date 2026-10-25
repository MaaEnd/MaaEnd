package autoessence

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/resource"
)

// locationKeysFile is the table from locations.json level_id to AutoEssence RegionNodes keys. It sits
// next to locations.json and is the only copy of the table: tools/essence_filter/build_autoessence_tasks.py
// reads the same file to generate the region options.
//
// 用 level_id（WorldEnergyPointTable 的 levelId）而不是显示名：levelId 与地区一一对应，不会随中文文案
// 或翻译改动而变化；显示名只在报错信息里用。
const locationKeysFile = "location_keys.json"

var locationLevelToKey map[string]string // level_id -> RegionNodes key, filled by loadLocationKeys

func loadLocationKeys(dataDir string) error {
	if locationLevelToKey != nil {
		return nil
	}
	var keys map[string]string
	if err := resource.ReadJsonResource(filepath.Join(dataDir, locationKeysFile), &keys); err != nil {
		return err
	}
	if len(keys) == 0 {
		return fmt.Errorf("%s has no location keys", locationKeysFile)
	}
	for levelID, key := range keys {
		if key == "" {
			return fmt.Errorf("%s: empty location key for %s", locationKeysFile, levelID)
		}
	}
	locationLevelToKey = keys
	return nil
}

// LocationKeyByLevel returns the pipeline location key for a locations.json level_id.
func LocationKeyByLevel(levelID string) (string, bool) {
	key, ok := locationLevelToKey[levelID]
	return key, ok
}

// RegionLabelKey returns the interface i18n key of a location's region name ($global.region.*):
// the RegionNodes key without its map prefix (VF / WL), the same rule the option generator uses.
func RegionLabelKey(locationKey string) (string, bool) {
	for _, prefix := range []string{"VF", "WL"} {
		if name, ok := strings.CutPrefix(locationKey, prefix); ok && name != "" {
			return "global.region." + name, true
		}
	}
	return "", false
}

// SetAnchorNode returns GotoTriggerPointSetAnchor_<key>.
func SetAnchorNode(locationKey string) string {
	return "GotoTriggerPointSetAnchor_" + locationKey
}
