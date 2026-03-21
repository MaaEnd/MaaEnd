package autostockpile

import (
	"encoding/json"
	"fmt"
	"strings"
)

var autoStockpileDefaultPriceLimits = map[string]int{
	"ValleyIVTier1": 800,
	"ValleyIVTier2": 1200,
	"ValleyIVTier3": 1500,
	"WulingTier1":   1200,
	"WulingTier2":   1500,
}

func defaultPriceLimitForTier(tierID string) (int, bool) {
	threshold, ok := autoStockpileDefaultPriceLimits[tierID]
	return threshold, ok
}

func requireDefaultPriceLimitForTier(tierID string) (int, error) {
	threshold, ok := defaultPriceLimitForTier(tierID)
	if !ok {
		return 0, fmt.Errorf("missing default threshold for %s", tierID)
	}
	return threshold, nil
}

func priceLimitTierIDFromAttachKey(key string) (string, error) {
	const prefix = "price_limits_"
	if !strings.HasPrefix(key, prefix) {
		return "", fmt.Errorf("invalid price limit key %s", key)
	}

	remainder := strings.TrimPrefix(key, prefix)
	parts := strings.SplitN(remainder, ".", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", fmt.Errorf("invalid price limit key %s", key)
	}

	return parts[0] + parts[1], nil
}

func normalizePriceLimitThreshold(tierID string, threshold int) int {
	if threshold != 0 {
		return threshold
	}

	if defaultThreshold, ok := defaultPriceLimitForTier(tierID); ok {
		return defaultThreshold
	}

	return threshold
}

func parsePriceLimitOverrideValue(key string, data json.RawMessage) (int, error) {
	tierID, err := priceLimitTierIDFromAttachKey(key)
	if err != nil {
		return 0, err
	}

	var stringValue string
	if err := json.Unmarshal(data, &stringValue); err == nil {
		if strings.TrimSpace(stringValue) == "" {
			return requireDefaultPriceLimitForTier(tierID)
		}
	}

	threshold, err := parsePriceLimitValue(data)
	if err != nil {
		return 0, err
	}

	return normalizePriceLimitThreshold(tierID, threshold), nil
}

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

func resolveTierThreshold(tierID string, cfg SelectionConfig) int {
	if threshold, ok := cfg.PriceLimits[tierID]; ok && threshold > 0 {
		return threshold
	}
	return resolveFallbackThreshold(cfg.FallbackThreshold)
}

func resolveFallbackThreshold(raw int) int {
	if raw > 0 {
		return raw
	}
	return defaultFallbackBuyThreshold
}
