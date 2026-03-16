package autostockpile

import (
	"encoding/json"
	"fmt"
	"strings"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

var autoStockpileDefaultPriceLimits = map[string]int{
	"price_limits_ValleyIV.Tier1": 800,
	"price_limits_ValleyIV.Tier2": 1200,
	"price_limits_ValleyIV.Tier3": 1500,
	"price_limits_Wuling.Tier1":   1200,
	"price_limits_Wuling.Tier2":   1500,
}

func getSelectionConfigFromNode(ctx *maa.Context, nodeName string) (SelectionConfig, error) {
	region, _ := resolveGoodsRegion(ctx)

	raw, err := ctx.GetNodeJSON(nodeName)
	if err != nil {
		log.Error().Err(err).Str("component", "autostockpile").Str("node", nodeName).Msg("failed to get node json")
		return SelectionConfig{}, err
	}

	return parseSelectionConfigFromNodeJSON(raw, region)
}

// parseSelectionConfigFromNodeJSON 解析节点 attach 配置，并按当前地区提取有效阈值。
func parseSelectionConfigFromNodeJSON(raw string, region string) (SelectionConfig, error) {
	cfg := SelectionConfig{FallbackThreshold: defaultFallbackBuyThreshold}
	if raw == "" {
		return cfg, nil
	}

	var wrapper struct {
		Attach map[string]json.RawMessage `json:"attach"`
	}

	if err := json.Unmarshal([]byte(raw), &wrapper); err != nil {
		return SelectionConfig{}, err
	}

	attachJSON, err := json.Marshal(wrapper.Attach)
	if err != nil {
		return SelectionConfig{}, err
	}
	if err := json.Unmarshal(attachJSON, &cfg); err != nil {
		return SelectionConfig{}, err
	}

	if err := applyRegionScopedConfig(wrapper.Attach, region, &cfg); err != nil {
		return SelectionConfig{}, err
	}
	if cfg.FallbackThreshold <= 0 {
		cfg.FallbackThreshold = defaultFallbackBuyThreshold
	}

	effectiveJSON, err := json.Marshal(cfg)
	if err != nil {
		log.Warn().Err(err).Str("component", "autostockpile").Str("region", region).Msg("failed to marshal effective config")
	} else {
		log.Info().Str("component", "autostockpile").Str("region", region).Str("attach", string(attachJSON)).Str("effective_config", string(effectiveJSON)).Msg("attach config loaded")
	}

	return cfg, nil
}

// applyRegionScopedConfig 从扁平前缀配置中收集当前地区的阈值，并覆盖为当前地区的有效配置。
func applyRegionScopedConfig(attach map[string]json.RawMessage, region string, cfg *SelectionConfig) error {
	if cfg == nil || region == "" {
		return nil
	}

	priceLimits, err := collectRegionPriceLimits(attach, region)
	if err != nil {
		return err
	}
	if len(priceLimits) == 0 {
		return nil
	}

	cfg.PriceLimits = priceLimits
	cfg.FallbackThreshold = minPositiveThreshold(priceLimits)
	return nil
}

// collectRegionPriceLimits 将形如 price_limits_ValleyIV.Tier1 的扁平 attach 字段收集为当前地区的价格阈值表。
func collectRegionPriceLimits(attach map[string]json.RawMessage, region string) (PriceLimitConfig, error) {
	prefix := fmt.Sprintf("price_limits_%s.", region)
	priceLimits := make(PriceLimitConfig)

	for key, value := range attach {
		if !strings.HasPrefix(key, prefix) {
			continue
		}

		tier := strings.TrimPrefix(key, prefix)
		if tier == "" {
			return nil, fmt.Errorf("%s: missing tier suffix", key)
		}

		threshold, err := parsePriceLimitOverrideValue(key, value)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", key, err)
		}

		priceLimits[region+tier] = threshold
	}

	return priceLimits, nil
}

func parsePriceLimitOverrideValue(key string, data json.RawMessage) (int, error) {
	var stringValue string
	if err := json.Unmarshal(data, &stringValue); err == nil {
		if strings.TrimSpace(stringValue) == "" {
			threshold, ok := autoStockpileDefaultPriceLimits[key]
			if !ok {
				return 0, fmt.Errorf("missing default threshold")
			}
			return threshold, nil
		}
	}

	return parsePriceLimitValue(data)
}

// minPositiveThreshold 返回价格阈值中的最小正值，用作当前地区的默认 fallback。
func minPositiveThreshold(priceLimits PriceLimitConfig) int {
	min := 0
	for _, threshold := range priceLimits {
		if threshold <= 0 {
			continue
		}
		if min == 0 || threshold < min {
			min = threshold
		}
	}
	if min > 0 {
		return min
	}
	return defaultFallbackBuyThreshold
}
