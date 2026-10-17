package autostockpile

import "github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"

// claimMinBuyEligibility 判断当前地区是否仍可执行「至少购买一个」的保底降级。
// 调用它即代表当前地区执行了一次购买决策，因此会把保底资格锁定到本次运行首个执行决策的地区：
// 后续地区调用时缓存已被占用，返回 false，不再降级购买。
//
// EdgeCase 当调度券不足时可能出现「当前地区已被锁定，但无法购买」的情况，因少见此处不做额外判断。
func claimMinBuyEligibility(minBuy bool, region string) bool {
	if !minBuy {
		return false
	}
	if cached := getMinBuyRegion(); cached != "" {
		return cached == region
	}

	setMinBuyRegion(region)
	return true
}

// resolveMinBuyFallback 在正常选品失败且当前地区保有保底资格时，
// 将决策降级为当前地区的最低价商品（数量 1），用于完成每日任务。
// minBuyEnabled 由 claimMinBuyEligibility 解析，已包含地区锁定判断。
// 返回 true 表示已应用降级，调用方应改用返回的 SelectionResult 与 quantityDecision。
func resolveMinBuyFallback(selection SelectionResult, data RecognitionData, minBuyEnabled bool, reason string) (SelectionResult, quantityDecision, bool) {
	if selection.Selected || !minBuyEnabled {
		return selection, quantityDecision{}, false
	}

	cheapest, err := selectCheapestProduct(data)
	if err != nil || !cheapest.Selected {
		return selection, quantityDecision{}, false
	}

	return cheapest, quantityDecision{
		Mode:   quantityModeSwipeSpecificQuantity,
		Target: 1,
		Reason: reason,
	}, true
}

// selectCheapestProduct 在全部已识别商品中选出价格最低的商品；价格相同时按名称稳定排序。
// 与 selectBestProduct 的按阈值/利润选品不同，本函数用于「至少购买一个」的强制降级。
func selectCheapestProduct(data RecognitionData) (SelectionResult, error) {
	if len(data.Goods) == 0 {
		return SelectionResult{Selected: false, Reason: i18n.T("autostockpile.no_goods_recognized")}, nil
	}

	cheapest := data.Goods[0]
	for _, goods := range data.Goods[1:] {
		if goods.Price < cheapest.Price || (goods.Price == cheapest.Price && goods.Name < cheapest.Name) {
			cheapest = goods
		}
	}

	// Threshold 刻意不填：来源已由 Source 显式表达，展示层不再依赖它。
	return SelectionResult{
		Selected:      true,
		ProductID:     cheapest.ID,
		ProductName:   cheapest.Name,
		CanonicalName: cheapest.Tier,
		CurrentPrice:  cheapest.Price,
		Source:        selectionSourceMinBuy,
	}, nil
}
