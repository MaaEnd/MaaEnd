package creditshopping

// recordCatalog 货架记录用全物品识别（与 CreditShoppingRecordCatalogItems 一致，不看购买选项）。
var recordCatalog = func() []recordCatalogItem {
	out := make([]recordCatalogItem, 0, len(creditShoppingItemCatalog))
	for _, item := range creditShoppingItemCatalog {
		out = append(out, recordCatalogItem{
			ID:                item.CaseID,
			Name:              item.Name,
			RecognitionItemID: item.RecognitionItemID,
		})
	}
	return out
}()
