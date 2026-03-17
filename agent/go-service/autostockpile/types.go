package autostockpile

import (
	"encoding/json"
	"fmt"
	"strconv"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

var (
	_ maa.CustomActionRunner      = &SelectItemAction{}
	_ maa.CustomRecognitionRunner = &ItemValueChangeRecognition{}
)

// SelectItemAction 根据识别结果执行商品选择动作。
type SelectItemAction struct{}

// ItemValueChangeRecognition 负责识别商品及其价格信息。
type ItemValueChangeRecognition struct{}

// RecognitionResult 表示识别阶段输出的结构化结果。
type RecognitionResult struct {
	Overflow       bool        `json:"overflow"`
	OverflowAmount int         `json:"overflow_amount"`
	Sunday         bool        `json:"sunday"`
	Goods          []GoodsItem `json:"Goods"`
}

// GoodsItem 表示一次识别得到的单个商品信息。
type GoodsItem struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Tier  string `json:"tier"`
	Price int    `json:"price"`
}

// SelectionResult 表示商品选择逻辑的决策结果。
type SelectionResult struct {
	Selected      bool
	ProductID     string
	ProductName   string
	CanonicalName string
	Threshold     int
	CurrentPrice  int
	Score         int
	Reason        string
}

// SelectionConfig 表示 AutoStockpile 的商品选择配置。
type SelectionConfig struct {
	Strategy          string           `json:"strategy"`
	OverflowMode      bool             `json:"overflow_mode"`
	SundayMode        bool             `json:"sunday_mode"`
	FallbackThreshold int              `json:"fallback_threshold"`
	PriceLimits       PriceLimitConfig `json:"price_limits"`
}

// PriceLimitConfig 按档位 ID 保存商品购买阈值。
type PriceLimitConfig map[string]int

// UnmarshalJSON 支持将数字或数字字符串形式的阈值反序列化为 PriceLimitConfig。
func (c *PriceLimitConfig) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		*c = nil
		return nil
	}

	raw := make(map[string]json.RawMessage)
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	parsed := make(PriceLimitConfig, len(raw))
	for key, value := range raw {
		threshold, err := parsePriceLimitValue(value)
		if err != nil {
			return fmt.Errorf("price_limits.%s: %w", key, err)
		}
		parsed[key] = normalizePriceLimitThreshold(key, threshold)
	}

	*c = parsed
	return nil
}

func parsePriceLimitValue(data json.RawMessage) (int, error) {
	var intValue int
	if err := json.Unmarshal(data, &intValue); err == nil {
		return intValue, nil
	}

	var stringValue string
	if err := json.Unmarshal(data, &stringValue); err == nil {
		parsed, parseErr := strconv.Atoi(stringValue)
		if parseErr != nil {
			return 0, fmt.Errorf("invalid integer string %q", stringValue)
		}
		return parsed, nil
	}

	return 0, fmt.Errorf("must be an integer or integer string")
}

// ThresholdConfig 表示匹配与定价阶段使用的阈值配置。
type ThresholdConfig struct {
	FallbackThreshold int              `json:"fallback_threshold"`
	PriceLimits       PriceLimitConfig `json:"price_limits"`
}

// ItemMatchResult 表示 OCR 商品名与规范商品名的匹配结果。
type ItemMatchResult struct {
	OCRName       string
	CanonicalName string
	TierID        string
	EditDistance  int
	Threshold     int
	Matched       bool
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func minInt(a, b, c int) int {
	if a < b {
		if a < c {
			return a
		}
		return c
	}
	if b < c {
		return b
	}
	return c
}

const (
	defaultFallbackBuyThreshold = 800
)
