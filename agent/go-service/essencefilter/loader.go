package essencefilter

import (
	"encoding/json"
	"os"
)

// LoadWeaponDatabase - 加载武器数据库
func LoadWeaponDatabase(filepath string) error {
	data, err := os.ReadFile(filepath)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, &weaponDB)
}

// LoadMatcherConfig - 加载匹配器配置
func LoadMatcherConfig(filepath string) error {
	data, err := os.ReadFile(filepath)
	if err != nil {
		return err
	}

	return json.Unmarshal(data, &matcherConfig)
}
