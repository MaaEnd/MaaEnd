package ims

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/iconqty"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/maafocus"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

const (
	// focusItemIconSizePx is the Focus HTML <img> width/height in px.
	focusItemIconSizePx = 16
)

type focusItemRow struct {
	ItemID   string
	Name     string
	Quantity int
	IconSrc  string
}

// reportSyncedItems prints one HTML Focus summary for A2 hits (sorted by name).
func reportSyncedItems(ctx *maa.Context, items map[string]int) int {
	return reportItemFocusSummary(ctx, "ims.sync_item_summary", items)
}

// reportAddedItems prints one HTML Focus summary for A3 deltas (sorted by name).
// Quantities are positive gains; callers should aggregate same item_id first.
func reportAddedItems(ctx *maa.Context, items map[string]int) int {
	return reportItemFocusSummary(ctx, "ims.add_item_summary", items)
}

func reportItemFocusSummary(ctx *maa.Context, templateKey string, items map[string]int) int {
	if ctx == nil || len(items) == 0 {
		return 0
	}
	rows := make([]focusItemRow, 0, len(items))
	for itemID, quantity := range items {
		rows = append(rows, focusItemRow{
			ItemID:   itemID,
			Name:     iconqty.ItemDisplayName(itemID),
			Quantity: quantity,
			IconSrc:  itemIconSrc(itemID),
		})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Name != rows[j].Name {
			return rows[i].Name < rows[j].Name
		}
		return rows[i].ItemID < rows[j].ItemID
	})
	html := i18n.RenderHTML(templateKey, map[string]any{
		"Items":    rows,
		"IconSize": focusItemIconSizePx,
	})
	maafocus.Print(ctx, html)
	return len(rows)
}

// itemIconSrc returns an install-root-relative path for MXU Focus <img src>.
// Runtime layout is install/{mxu, resource}/… — never prefix with assets/.
// MXU resolves the path against basePath and inlines the image.
func itemIconSrc(itemID string) string {
	itemID = strings.TrimSpace(itemID)
	if itemID == "" {
		return ""
	}
	catalog, err := loadRecognitionItems()
	if err != nil {
		return ""
	}
	meta, ok := catalog[itemID]
	if !ok {
		return ""
	}
	iconID := strings.TrimSpace(meta.IconID)
	if iconID == "" || meta.Rarity <= 0 {
		return ""
	}
	return filepath.ToSlash(filepath.Join(
		"resource",
		"image",
		"IconRecognition",
		fmt.Sprintf("%d", meta.Rarity),
		iconID+".png",
	))
}
