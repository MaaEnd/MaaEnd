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

// LoadPresets - 加载预设配置
func LoadPresets(filepath string) ([]FilterPreset, error) {
	data, err := os.ReadFile(filepath)
	if err != nil {
		return nil, err
	}

	var config struct {
		Presets []FilterPreset `json:"presets"`
	}

	err = json.Unmarshal(data, &config)
	return config.Presets, err
}

// LoadMatcherConfig - 加载匹配器配置
func LoadMatcherConfig(filepath string) error {
	data, err := os.ReadFile(filepath)
	if err != nil {
		return err
	}

	return json.Unmarshal(data, &matcherConfig)
}
