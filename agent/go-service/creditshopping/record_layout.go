package creditshopping

import (
	"sort"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const (
	recordRowClusterGapY    = 80
	recordMaxShelfSlots     = 10
	recordSlotMatchColSlack = 24
	recordSlotMatchRowSlack = 24
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

func recordAbsInt(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// recordHitBelongsToSlot 判定商品模板中心是否落在槽位锚框附近（同行 + 水平对齐，720p）。
func recordHitBelongsToSlot(slot maa.Rect, hit itemPositionHit) bool {
	if !recordRectValid(slot) || !recordRectValid(hit.Box) {
		return false
	}
	scx, scy := recordRectCenter(slot)
	icx, icy := recordRectCenter(hit.Box)
	maxDx := slot[2]/2 + recordSlotMatchColSlack
	maxDy := slot[3]/2 + recordSlotMatchRowSlack
	return recordAbsInt(icx-scx) <= maxDx && recordAbsInt(icy-scy) <= maxDy
}

type recordSlotItemPair struct {
	slotIdx int
	itemIdx int
	distSq  int
}

// recordMatchItemsToSlots 将商品模板命中挂到槽位锚框（每槽最多一件，每件最多占一槽；拒绝几何上不属于该槽的命中）。
func recordMatchItemsToSlots(slotBoxes []maa.Rect, items []itemPositionHit) []itemPositionHit {
	out := make([]itemPositionHit, len(slotBoxes))
	if len(slotBoxes) == 0 || len(items) == 0 {
		return out
	}
	pairs := make([]recordSlotItemPair, 0, len(slotBoxes)*len(items))
	for si, slot := range slotBoxes {
		scx, scy := recordRectCenter(slot)
		for ii, it := range items {
			if !recordHitBelongsToSlot(slot, it) {
				continue
			}
			icx, icy := recordRectCenter(it.Box)
			dx := icx - scx
			dy := icy - scy
			pairs = append(pairs, recordSlotItemPair{
				slotIdx: si,
				itemIdx: ii,
				distSq:  dx*dx + dy*dy,
			})
		}
	}
	sort.Slice(pairs, func(i, j int) bool {
		return pairs[i].distSq < pairs[j].distSq
	})
	slotUsed := make([]bool, len(slotBoxes))
	itemUsed := make([]bool, len(items))
	for _, p := range pairs {
		if slotUsed[p.slotIdx] || itemUsed[p.itemIdx] {
			continue
		}
		slotUsed[p.slotIdx] = true
		itemUsed[p.itemIdx] = true
		out[p.slotIdx] = items[p.itemIdx]
	}
	return out
}
