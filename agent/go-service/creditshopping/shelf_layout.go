package creditshopping

import (
	"image"
	"sort"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

// 720p 货架槽位：PC 一屏两行 7+3；ADB 一屏仅见一排名称，需滑动后见另一排（上 6 + 下 4）。
const (
	shelfSlotCount     = 10
	pcTopRowSlots      = 7
	pcBottomRowSlots   = 3
	adbTopRowSlots     = 6
	adbBottomRowSlots  = 4
	adbBottomSlotStart  = 6
	adbShelfNameRowSplitY = 400 // 首屏名称 Y<400；滑动后第二排名称 Y>=400
	rowClusterGapY        = 80
)

type slotAssignMode int

const (
	slotAssignPC slotAssignMode = iota
	slotAssignADBTop
	slotAssignADBBottom
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

func hitsForMode(hits []ocrNameHit, mode slotAssignMode) []ocrNameHit {
	if len(hits) == 0 {
		return nil
	}
	rows := clusterRowsByY(hits)
	switch mode {
	case slotAssignPC:
		return flattenRowLimits(rows, []int{pcTopRowSlots, pcBottomRowSlots})
	case slotAssignADBTop:
		// 首屏只见第一排名称与折扣；屏内仅一排，按 X 排序取前 6，不做 Y 聚类分行。
		return capHits(sortHitsByX(hits), adbTopRowSlots)
	case slotAssignADBBottom:
		// 滑动后只见第二排名称与折扣；屏内仅一排，按 X 排序取前 4。
		return capHits(sortHitsByX(hits), adbBottomRowSlots)
	default:
		return nil
	}
}

func sortHitsByX(hits []ocrNameHit) []ocrNameHit {
	sorted := append([]ocrNameHit(nil), hits...)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Box[0] < sorted[j].Box[0]
	})
	return sorted
}

func flattenRowLimits(rows [][]ocrNameHit, limits []int) []ocrNameHit {
	var out []ocrNameHit
	for i, lim := range limits {
		if i >= len(rows) {
			break
		}
		out = append(out, capHits(rows[i], lim)...)
	}
	return out
}

func capHits(hits []ocrNameHit, max int) []ocrNameHit {
	if max <= 0 || len(hits) <= max {
		return hits
	}
	log.Warn().
		Str("component", component).
		Int("hits", len(hits)).
		Int("max", max).
		Msg("shelf layout: truncating extra hits in row")
	return hits[:max]
}

func slotStartForMode(mode slotAssignMode) int {
	switch mode {
	case slotAssignADBBottom:
		return adbBottomSlotStart
	default:
		return 0
	}
}

func filterADBShelfNameHits(hits []ocrNameHit, mode slotAssignMode) []ocrNameHit {
	out := make([]ocrNameHit, 0, len(hits))
	for _, h := range hits {
		y := h.Box[1]
		keep := false
		switch mode {
		case slotAssignADBTop:
			keep = y < adbShelfNameRowSplitY
		case slotAssignADBBottom:
			keep = y >= adbShelfNameRowSplitY
		default:
			keep = true
		}
		if keep {
			out = append(out, h)
			continue
		}
		log.Debug().
			Str("component", component).
			Str("ocr_text", h.Text).
			Int("box_y", y).
			Int("mode", int(mode)).
			Msg("shelf layout adb: drop hit outside target row")
	}
	return out
}

func buildSlotRecords(ctx *maa.Context, img image.Image, hits []ocrNameHit, mode slotAssignMode) []SlotRecord {
	if mode == slotAssignADBTop || mode == slotAssignADBBottom {
		hits = filterADBShelfNameHits(hits, mode)
	}
	picked := hitsForMode(hits, mode)
	if len(picked) == 0 {
		return nil
	}
	start := slotStartForMode(mode)
	out := make([]SlotRecord, 0, len(picked))
	for i, hit := range picked {
		itemID, ok := matchCreditItemID(hit.Text)
		if !ok {
			log.Warn().
				Str("component", component).
				Int("slot", start+i).
				Str("ocr_text", hit.Text).
				Msg("shelf scan: unmatched item name, skip slot")
			continue
		}
		out = append(out, SlotRecord{
			Slot:     start + i,
			ItemID:   itemID,
			Discount: recordDiscountAtNameBox(ctx, img, hit.Box),
		})
	}
	return out
}

func mergeSlotRecordsByPosition(parts ...[]SlotRecord) []SlotRecord {
	bySlot := make(map[int]SlotRecord, shelfSlotCount)
	for _, part := range parts {
		for _, s := range part {
			if s.Slot < 0 || s.Slot >= shelfSlotCount {
				continue
			}
			bySlot[s.Slot] = s
		}
	}
	out := make([]SlotRecord, 0, len(bySlot))
	for slot := 0; slot < shelfSlotCount; slot++ {
		if s, ok := bySlot[slot]; ok {
			out = append(out, s)
		}
	}
	return out
}
