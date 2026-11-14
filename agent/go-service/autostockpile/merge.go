package autostockpile

// mergeGoodsPages 按商品 ID 合并多屏扫描结果：同一 ID 保留首次出现的条目，
// 并返回仅在第 N 屏（0-based）才扫到的 ID → 露出该商品需要下滑的次数 N。
func mergeGoodsPages(pages [][]GoodsItem) (goods []GoodsItem, swipeCountToReveal map[string]int) {
	total := 0
	for _, page := range pages {
		total += len(page)
	}

	seen := make(map[string]struct{}, total)
	goods = make([]GoodsItem, 0, total)
	swipeCountToReveal = make(map[string]int)

	for pageIndex, page := range pages {
		for _, item := range page {
			if _, ok := seen[item.ID]; ok {
				continue
			}
			seen[item.ID] = struct{}{}
			goods = append(goods, item)
			// pageIndex 即发现该屏所需的下滑次数：每多扫一屏必然多下滑一次。
			if pageIndex > 0 {
				swipeCountToReveal[item.ID] = pageIndex
			}
		}
	}

	return goods, swipeCountToReveal
}
