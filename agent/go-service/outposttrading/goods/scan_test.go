package goods

import (
	"errors"
	"image"
	"testing"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/outposttrading/internal/ocrmatch"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

// 一格库存读不出来时只把这一格记为库存未知，其余格子照常可用；
// 以前整页报错，选择与耗尽两个分支都拿不到结果，只能等超时
func TestBuildStockPageItemsKeepsUnreadableStockAsUnknown(t *testing.T) {
	offsets := stockCellOffsets{
		Name:     maa.Rect{34, -59, 164, 16},
		Quantity: maa.Rect{33, -6, 80, 7},
		Click:    maa.Rect{42, -58, 100, 34},
	}
	anchors := []maa.Rect{{100, 100, 17, 17}, {500, 100, 17, 17}, {900, 100, 17, 17}}
	groups := []itemPriorityGroup{
		{ItemID: "a", Candidates: []string{"甲货"}},
		{ItemID: "b", Candidates: []string{"乙货"}},
		{ItemID: "c", Candidates: []string{"丙货"}},
	}
	ocrItems := []ocrmatch.Item{
		{Text: "甲货", Box: maa.Rect{140, 45, 50, 20}},
		{Text: "16", Box: maa.Rect{140, 98, 20, 15}},
		{Text: "乙货", Box: maa.Rect{540, 45, 50, 20}},
		{Text: "丙货", Box: maa.Rect{940, 45, 50, 20}},
		{Text: "0", Box: maa.Rect{940, 98, 10, 15}},
	}
	fallbackCalls := 0
	fallback := func(maa.Rect) (string, error) {
		fallbackCalls++
		return "", errors.New("no stock quantity recognized")
	}

	items, err := buildStockPageItems(ocrItems, anchors, groups, offsets, image.Rect(0, 0, 1280, 720), fallback)
	if err != nil {
		t.Fatalf("buildStockPageItems() error = %v", err)
	}
	if fallbackCalls != 1 {
		t.Fatalf("fallback calls = %d, want 1", fallbackCalls)
	}
	got := make(map[string]stockPageItem, len(items))
	for _, item := range items {
		got[item.ItemID] = item
	}
	if item := got["a"]; !item.StockKnown || item.Quantity != 16 {
		t.Fatalf("item a = %+v, want known stock 16", item)
	}
	if item := got["b"]; item.StockKnown || item.ClickBox.Width() <= 0 {
		t.Fatalf("item b = %+v, want unknown stock with a click box", item)
	}
	if item := got["c"]; !item.StockKnown || item.Quantity != 0 {
		t.Fatalf("item c = %+v, want known stock 0", item)
	}
}
