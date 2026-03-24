package autostockpile

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

func getSelectionConfigFromNode(ctx *maa.Context, nodeName string, region string) (SelectionConfig, AbortReason, error) {
	if region == "" {
		return SelectionConfig{}, AbortReasonSelectionConfigInvalidFatal, fmt.Errorf("region is empty")
	}

	node, err := ctx.GetNode(nodeName)
	if err != nil {
		log.Error().Err(err).Str("component", autoStockpileComponent).Str("node", nodeName).Msg("failed to get node")
		return SelectionConfig{}, AbortReasonSelectionConfigInvalidFatal, err
	}

	cfg, err := parseSelectionConfigFromAttach(node.Attach, region)
	if err != nil {
		if isThresholdConfigError(err) {
			return SelectionConfig{}, AbortReasonThresholdConfigInvalidFatal, err
		}
		return SelectionConfig{}, AbortReasonSelectionConfigInvalidFatal, err
	}

	return cfg, AbortReasonNone, nil
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
		log.Warn().Err(err).Str("component", autoStockpileComponent).Str("region", region).Msg("failed to marshal effective config")
	} else {
		log.Info().Str("component", autoStockpileComponent).Str("region", region).Str("attach", string(attachJSON)).Str("effective_config", string(effectiveJSON)).Msg("attach config loaded")
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
	if len(priceLimits) > 0 {
		cfg.PriceLimits = priceLimits
		cfg.FallbackThreshold = minPositiveThreshold(priceLimits)
	}

	reserveStockBill, found, err := collectRegionReserveStockBill(attach, region)
	if err != nil {
		return err
	}
	if found {
		cfg.ReserveStockBill = reserveStockBill
	}

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

// collectRegionReserveStockBill 从扁平 attach 中提取当前地区的库存账单保留配置。
// 查找形如 reserve_stock_bill_ValleyIV 的 key，将其值乘以 10000 转换为实际数值。
func collectRegionReserveStockBill(attach map[string]json.RawMessage, region string) (value int, found bool, err error) {
	key := fmt.Sprintf("reserve_stock_bill_%s", region)
	rawValue, exists := attach[key]
	if !exists {
		return 0, false, nil
	}

	parsedValue, err := parsePositiveThresholdValue(key, rawValue)
	if err != nil {
		return 0, true, err
	}

	if parsedValue > math.MaxInt/10000 {
		return 0, true, newThresholdConfigError(key, fmt.Errorf("value %d too large (max %d)", parsedValue, math.MaxInt/10000))
	}

	return parsedValue * 10000, true, nil
}
