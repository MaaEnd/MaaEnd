package autostockpile

// mergeGoodsByID 合并两页商品：保留首屏条目，仅追加次屏新 ID，并返回仅次屏出现的 ID 列表。
func mergeGoodsByID(page0, page1 []GoodsItem) (goods []GoodsItem, page1OnlyIDs []string) {
	seen := make(map[string]struct{}, len(page0)+len(page1))
	goods = make([]GoodsItem, 0, len(page0)+len(page1))
	page1OnlyIDs = make([]string, 0)

	for _, item := range page0 {
		if item.ID == "" {
			continue
		}
		if _, ok := seen[item.ID]; ok {
			continue
		}
		seen[item.ID] = struct{}{}
		goods = append(goods, item)
	}

	for _, item := range page1 {
		if item.ID == "" {
			continue
		}
		if _, ok := seen[item.ID]; ok {
			continue
		}
		seen[item.ID] = struct{}{}
		goods = append(goods, item)
		page1OnlyIDs = append(page1OnlyIDs, item.ID)
	}

	return goods, page1OnlyIDs
}
