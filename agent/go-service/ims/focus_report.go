package ims

import (
	"sort"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/iconqty"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/maafocus"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

const (
	// focusItemIconSizePx is the size (in px) that item icons are downscaled to
	// before being inlined into the Focus HTML, and the <img> width/height.
	focusItemIconSizePx = 16
)

type focusItemRow struct {
	ItemID   string
	Name     string
	Quantity int
	IconSrc  string
}

// filterSyncedReportItems selects A2 UI entries without changing recognized or cached quantities.
func filterSyncedReportItems(items map[string]int, itemIDs []string) map[string]int {
	if len(itemIDs) == 0 {
		return items
	}
	report := make(map[string]int, len(itemIDs))
	for _, itemID := range itemIDs {
		if quantity, ok := items[itemID]; ok {
			report[itemID] = quantity
		}
	}
	return report
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
