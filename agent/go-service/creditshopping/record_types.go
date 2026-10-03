package creditshopping

import maa "github.com/MaaXYZ/maa-framework-go/v4"

const (
	component               = "creditshopping"
	recordSlotNameUnknown   = "unknown"
	recordSlotItemIDUnknown = "unknown"
)

// SlotRecord 是写入快照 JSON 的单格货架记录（slot 按屏幕位置排序，从 0 起）。
type SlotRecord struct {
	Slot     int    `json:"slot"`
	Name     string `json:"name"`
	ID       string `json:"id,omitempty"`
	Discount string `json:"discount"`
}

type itemPositionHit struct {
	Box  maa.Rect
	Name string
	ID   string
}

type recordCatalogItem struct {
	Node string
	ID   string
	Name string
}
