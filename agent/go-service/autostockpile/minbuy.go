package autostockpile

import "github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"

// syncMinBuyRegion 返回任务内 MinBuyRegion 缓存值；缓存为空时将其设为当前地区。
// 保证「至少购买一个」只锁定本次运行首个访问的地区。
func syncMinBuyRegion(region string) string {
	if cached := getMinBuyRegion(); cached != "" {
		return cached
	}
	setMinBuyRegion(region)
	return region
}

// resolveMinBuyFallback 在正常选品失败且启用「至少购买一个」时，
// 将决策降级为当前地区的最低价商品（数量 1），用于完成每日任务。
// 只有 MinBuyRegion 等于当前地区时才允许降级，其他地区不受影响。
// 返回 true 表示已应用降级，调用方应改用返回的 SelectionResult 与 quantityDecision。
func resolveMinBuyFallback(selection SelectionResult, data RecognitionData, region string, minBuyEnabled bool, reason string) (SelectionResult, quantityDecision, bool) {
	if selection.Selected || !minBuyEnabled {
		return selection, quantityDecision{}, false
	}
	if minBuyRegion := syncMinBuyRegion(region); minBuyRegion != region {
		return selection, quantityDecision{}, false
	}

	cheapest, err := SelectCheapestProduct(data)
	if err != nil || !cheapest.Selected {
		return selection, quantityDecision{}, false
	}

	return cheapest, quantityDecision{
		Mode:   quantityModeSwipeSpecificQuantity,
		Target: 1,
		Reason: reason,
	}, true
}

// SelectCheapestProduct 在全部已识别商品中选出价格最低的商品；价格相同时按名称稳定排序。
// 与 SelectBestProduct 的按阈值/利润选品不同，本函数用于「至少购买一个」的强制降级。
func SelectCheapestProduct(data RecognitionData) (SelectionResult, error) {
	if len(data.Goods) == 0 {
		return SelectionResult{Selected: false, Reason: i18n.T("autostockpile.no_goods_recognized")}, nil
	}

	cheapest := data.Goods[0]
	for _, goods := range data.Goods[1:] {
		if goods.Price < cheapest.Price || (goods.Price == cheapest.Price && goods.Name < cheapest.Name) {
			cheapest = goods
		}
	}

	return SelectionResult{
		Selected:      true,
		ProductID:     cheapest.ID,
		ProductName:   cheapest.Name,
		CanonicalName: cheapest.Tier,
		CurrentPrice:  cheapest.Price,
	}, nil
}
