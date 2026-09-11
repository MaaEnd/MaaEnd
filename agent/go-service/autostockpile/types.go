package autostockpile

import (
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
	// SecondPageOnlyIDs 仅在下滑后的第二屏扫到、首屏没有的商品 ID。
	// 选中这类商品后需先下滑再点击；勿理解成“第一页专属”。
	SecondPageOnlyIDs []string `json:"SecondPageOnlyIDs,omitempty"`
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

// selectionSource 标识一次选品的来源，决定展示的模式文案。
// 禁止用「价格 / 阈值 / 溢出」等数值关系反推来源：那等于在展示层重新实现一遍
// 选品分支，并与调用方是否启用「至少购买一个」隐式耦合。
type selectionSource string

const (
	// selectionSourceThreshold：正常阈值选品，价格严格低于阈值。
	selectionSourceThreshold selectionSource = "Threshold"
	// selectionSourceOverflow：防溢出放行选品，价格不低于阈值但额度已溢出。
	selectionSourceOverflow selectionSource = "Overflow"
	// selectionSourceMinBuy：「至少购买一个」降级购买当前地区最低价商品。
	selectionSourceMinBuy selectionSource = "MinBuy"
)

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
	// Source 仅在 Selected 为 true 时有效，由 selectBestProduct /
	// selectCheapestProduct 在构造时确定。
	Source selectionSource
}

// SelectionConfig 表示 AutoStockpile 的商品选择配置。
type SelectionConfig struct {
	PriceLimits PriceLimitConfig `json:"price_limits"`
}

// PriceLimitConfig 按档位 ID 保存商品购买阈值。
type PriceLimitConfig map[string]int

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
