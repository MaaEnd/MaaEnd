package creditshopping

import (
	"sort"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const (
	recordRowClusterGapY = 80
	recordMaxShelfSlots  = 10
)

func recordRectCenter(r maa.Rect) (int, int) {
	return r[0] + r[2]/2, r[1] + r[3]/2
}

func recordRectCenterY(r maa.Rect) int {
	return r[1] + r[3]/2
}

func recordClusterRowsByY(hits []itemPositionHit) [][]itemPositionHit {
	if len(hits) == 0 {
		return nil
	}
	sorted := append([]itemPositionHit(nil), hits...)
	sort.Slice(sorted, func(i, j int) bool {
		cyI := recordRectCenterY(sorted[i].Box)
		cyJ := recordRectCenterY(sorted[j].Box)
		if cyI != cyJ {
			return cyI < cyJ
		}
		return sorted[i].Box[0] < sorted[j].Box[0]
	})
	var rows [][]itemPositionHit
	cur := []itemPositionHit{sorted[0]}
	for i := 1; i < len(sorted); i++ {
		if recordRectCenterY(sorted[i].Box)-recordRectCenterY(sorted[i-1].Box) > recordRowClusterGapY {
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

// recordOrderHitsByPosition 先上后下、同行从左到右；不假定固定行宽（7+3 / 5+5 等）。
func recordOrderHitsByPosition(hits []itemPositionHit) []itemPositionHit {
	rows := recordClusterRowsByY(hits)
	if len(rows) == 0 {
		return nil
	}
	n := 0
	for _, row := range rows {
		n += len(row)
	}
	out := make([]itemPositionHit, 0, n)
	for _, row := range rows {
		out = append(out, row...)
	}
	if len(out) > recordMaxShelfSlots {
		log.Warn().
			Str("component", component).
			Int("hits", len(out)).
			Int("max", recordMaxShelfSlots).
			Msg("record: truncating extra hits on shelf")
		out = out[:recordMaxShelfSlots]
	}
	return out
}

func recordOrderBoxesByPosition(boxes []maa.Rect) []maa.Rect {
	if len(boxes) == 0 {
		return nil
	}
	hits := make([]itemPositionHit, len(boxes))
	for i, b := range boxes {
		hits[i] = itemPositionHit{Box: b}
	}
	ordered := recordOrderHitsByPosition(hits)
	out := make([]maa.Rect, len(ordered))
	for i, h := range ordered {
		out[i] = h.Box
	}
	return out
}

// recordMatchItemsToSlots 将商品模板命中按中心距离挂到 CreditIcon 槽位（每槽最多一件，每件最多占一槽）。
func recordMatchItemsToSlots(slotBoxes []maa.Rect, items []itemPositionHit) []itemPositionHit {
	out := make([]itemPositionHit, len(slotBoxes))
	used := make([]bool, len(items))
	for si, slot := range slotBoxes {
		scx, scy := recordRectCenter(slot)
		best := -1
		bestDist := int(^uint(0) >> 1)
		for ii, it := range items {
			if used[ii] {
				continue
			}
			icx, icy := recordRectCenter(it.Box)
			dx := icx - scx
			dy := icy - scy
			d := dx*dx + dy*dy
			if d < bestDist {
				bestDist = d
				best = ii
			}
		}
		if best >= 0 {
			used[best] = true
			out[si] = items[best]
		}
	}
	return out
}
