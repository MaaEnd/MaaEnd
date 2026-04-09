package autostockpile

import (
	"encoding/json"
	"fmt"
	"strings"
)

// AbortReason 表示识别阶段提前终止的稳定原因键。
type AbortReason string

const (
	AbortReasonNone                        AbortReason = "None"
	AbortReasonQuotaZeroSkip               AbortReason = "QuotaZeroSkip"
	AbortReasonRegionResolveFailedFatal    AbortReason = "RegionResolveFailedFatal"
	AbortReasonSelectionConfigInvalidFatal AbortReason = "SelectionConfigInvalidFatal"
	AbortReasonThresholdConfigInvalidFatal AbortReason = "ThresholdConfigInvalidFatal"
	AbortReasonGoodsTierInvalidFatal       AbortReason = "GoodsTierInvalidFatal"
	AbortReasonGoodsOCRUnavailableWarn     AbortReason = "GoodsOCRUnavailableWarn"
	AbortReasonQuotaUnavailableWarn        AbortReason = "QuotaUnavailableWarn"
)

const (
	abortReasonFatalSuffix = "Fatal"
	abortReasonWarnSuffix  = "Warn"
	abortReasonSkipSuffix  = "Skip"
)

var knownAbortReasons = []AbortReason{
	AbortReasonNone,
	AbortReasonQuotaZeroSkip,
	AbortReasonRegionResolveFailedFatal,
	AbortReasonSelectionConfigInvalidFatal,
	AbortReasonThresholdConfigInvalidFatal,
	AbortReasonGoodsTierInvalidFatal,
	AbortReasonGoodsOCRUnavailableWarn,
	AbortReasonQuotaUnavailableWarn,
}

func (r AbortReason) isFatal() bool {
	return strings.HasSuffix(string(r), abortReasonFatalSuffix)
}

func (r AbortReason) isWarn() bool {
	return strings.HasSuffix(string(r), abortReasonWarnSuffix)
}

func (r AbortReason) isSkip() bool {
	return strings.HasSuffix(string(r), abortReasonSkipSuffix)
}

// RecognitionResult 表示识别阶段输出的最终传输契约。
type RecognitionResult struct {
	Data        *RecognitionData `json:"Data"`
	AbortReason AbortReason      `json:"AbortReason"`
}

// RecognitionData 表示识别成功时传递给消费端的原始数据。
type RecognitionData struct {
	Quota QuotaInfo   `json:"Quota"`
	Goods []GoodsItem `json:"Goods"`
}

// QuotaInfo 表示额度识别结果。
type QuotaInfo struct {
	Current  int `json:"Current"`
	Overflow int `json:"Overflow"`
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
	PriceLimits PriceLimitConfig `json:"price_limits"`
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
		threshold, err := parsePositiveThresholdValue("price_limits."+key, value)
		if err != nil {
			return err
		}
		parsed[key] = threshold
	}

	*c = parsed
	return nil
}

// Validate 校验 RecognitionResult 是否满足新契约约束。
func (r RecognitionResult) Validate() error {
	if !isKnownAbortReason(r.AbortReason) {
		return fmt.Errorf("unknown abort reason %q", r.AbortReason)
	}

	if r.AbortReason == AbortReasonNone {
		if r.Data == nil {
			return fmt.Errorf("data must not be nil when abort reason is %q", AbortReasonNone)
		}
		return nil
	}

	if r.Data != nil {
		return fmt.Errorf("data must be nil when abort reason is %q", r.AbortReason)
	}

	return nil
}

func (r RecognitionResult) hasOverflow() bool {
	return r.Data != nil && r.Data.Quota.Overflow > 0
}

func isKnownAbortReason(reason AbortReason) bool {
	for _, candidate := range knownAbortReasons {
		if reason == candidate {
			return true
		}
	}

	return false
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
