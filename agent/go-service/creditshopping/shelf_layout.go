package creditshopping

import (
	"image"
	"sort"
	"strings"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const (
	// rowClusterGapY：相邻命中中心 Y 差超过此值（720p）则视为不同行。
	rowClusterGapY = 80
	// maxShelfSlots：信用商店单屏可见槽位上限。
	maxShelfSlots = 10
)

func rectCenterY(r maa.Rect) int {
	return r[1] + r[3]/2
}

// clusterRowsByY 按纵向间距将命中分为多行（上→下）。
func clusterRowsByY(hits []ocrNameHit) [][]ocrNameHit {
	if len(hits) == 0 {
		return nil
	}
	sorted := append([]ocrNameHit(nil), hits...)
	sort.Slice(sorted, func(i, j int) bool {
		cyI := rectCenterY(sorted[i].Box)
		cyJ := rectCenterY(sorted[j].Box)
		if cyI != cyJ {
			return cyI < cyJ
		}
		return sorted[i].Box[0] < sorted[j].Box[0]
	})
	var rows [][]ocrNameHit
	cur := []ocrNameHit{sorted[0]}
	for i := 1; i < len(sorted); i++ {
		if rectCenterY(sorted[i].Box)-rectCenterY(sorted[i-1].Box) > rowClusterGapY {
			rows = append(rows, cur)
			cur = nil
		}
		cur = append(cur, sorted[i])
	}
	rows = append(rows, cur)
	for i := range rows {
		sort.Slice(rows[i], func(a, b int) bool {
			return rows[i][a].Box[0] < rows[i][b].Box[0]
		})
	}
	return rows
}

// orderHitsByShelfPosition 按屏幕阅读顺序排列：先上后下、同行从左到右；不假定 7+3 / 5+5。
func orderHitsByShelfPosition(hits []ocrNameHit) []ocrNameHit {
	rows := clusterRowsByY(hits)
	if len(rows) == 0 {
		return nil
	}
	n := 0
	for _, row := range rows {
		n += len(row)
	}
	out := make([]ocrNameHit, 0, n)
	for _, row := range rows {
		out = append(out, row...)
	}
	if len(out) > maxShelfSlots {
		log.Warn().
			Str("component", component).
			Int("hits", len(out)).
			Int("max", maxShelfSlots).
			Msg("shelf layout: truncating extra hits on shelf")
		out = out[:maxShelfSlots]
	}
	return out
}

func buildSlotRecords(ctx *maa.Context, img image.Image, hits []ocrNameHit, adb bool) []SlotRecord {
	picked := orderHitsByShelfPosition(hits)
	if len(picked) == 0 {
		return nil
	}
	out := make([]SlotRecord, 0, len(picked))
	for i, hit := range picked {
		name := strings.TrimSpace(hit.Text)
		rec := SlotRecord{
			Slot:     i,
			Name:     name,
			ID:       hit.ID,
			Discount: recordDiscountAtNameBox(ctx, img, hit.Box, adb),
		}
		if rec.ID == "" {
			itemID, matched := matchCreditItemID(name)
			if !matched {
				log.Warn().
					Str("component", component).
					Int("slot", i).
					Str("name", name).
					Msg("shelf scan: unmatched item name, record without id")
			} else {
				rec.ID = itemID
			}
		}
		out = append(out, rec)
	}
	return out
}
