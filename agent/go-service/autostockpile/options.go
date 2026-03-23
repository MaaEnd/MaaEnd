package autostockpile

import (
	"encoding/json"
	"fmt"
	"strings"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

func getSelectionConfigFromNode(ctx *maa.Context, nodeName string) (SelectionConfig, error) {
	region, _ := resolveGoodsRegion(ctx)

	node, err := ctx.GetNode(nodeName)
	if err != nil {
		log.Error().Err(err).Str("component", "autostockpile").Str("node", nodeName).Msg("failed to get node")
		return SelectionConfig{}, err
	}

	return parseSelectionConfigFromAttach(node.Attach, region)
}

func parseSelectionConfigFromAttach(attach map[string]any, region string) (SelectionConfig, error) {
	cfg := SelectionConfig{FallbackThreshold: defaultFallbackBuyThreshold}
	if len(attach) == 0 {
		return cfg, nil
	}

	attachJSON, err := json.Marshal(attach)
	if err != nil {
		return SelectionConfig{}, err
	}
	if err := json.Unmarshal(attachJSON, &cfg); err != nil {
		return SelectionConfig{}, err
	}

	rawAttach, err := marshalAttachRawMessages(attach)
	if err != nil {
		return SelectionConfig{}, err
	}

	if err := applyRegionScopedConfig(rawAttach, region, &cfg); err != nil {
		return SelectionConfig{}, err
	}
	cfg.FallbackThreshold = resolveFallbackThreshold(cfg.FallbackThreshold)

	effectiveJSON, err := json.Marshal(cfg)
	if err != nil {
		log.Warn().Err(err).Str("component", "autostockpile").Str("region", region).Msg("failed to marshal effective config")
	} else {
		log.Info().Str("component", "autostockpile").Str("region", region).Str("attach", string(attachJSON)).Str("effective_config", string(effectiveJSON)).Msg("attach config loaded")
	}

	return cfg, nil
}

func marshalAttachRawMessages(attach map[string]any) (map[string]json.RawMessage, error) {
	if len(attach) == 0 {
		return nil, nil
	}

	rawAttach := make(map[string]json.RawMessage, len(attach))
	for key, value := range attach {
		rawValue, err := json.Marshal(value)
		if err != nil {
			return nil, err
		}
		rawAttach[key] = rawValue
	}

	return rawAttach, nil
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
