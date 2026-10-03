package creditshopping

import (
	"fmt"
	"sort"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/control"
	"github.com/rs/zerolog/log"
)

const (
	recordRowClusterGapY    = 80
	recordMaxShelfSlots     = 10
	recordSlotMatchColSlack = 24
)

// recordShelfLayout 货架骨架：上行格数 + 下行格数（720p 一屏内两行）。
type recordShelfLayout struct {
	topRow    int
	bottomRow int
}

func recordLayoutFromControlType(controlType string) recordShelfLayout {
	if controlType == control.CONTROL_TYPE_ADB {
		return recordShelfLayout{topRow: 5, bottomRow: 5}
	}
	return recordShelfLayout{topRow: 7, bottomRow: 3}
}

func recordLayoutLabel(layout recordShelfLayout) string {
	return fmt.Sprintf("%d+%d", layout.topRow, layout.bottomRow)
}

func recordRectCenter(r maa.Rect) (int, int) {
	return r[0] + r[2]/2, r[1] + r[3]/2
}

func recordRectCenterY(r maa.Rect) int {
	return r[1] + r[3]/2
}

func recordClusterRowsByYFromBoxes(boxes []maa.Rect) [][]maa.Rect {
	if len(boxes) == 0 {
		return nil
	}
	sorted := append([]maa.Rect(nil), boxes...)
	sort.Slice(sorted, func(i, j int) bool {
		cyI := recordRectCenterY(sorted[i])
		cyJ := recordRectCenterY(sorted[j])
		if cyI != cyJ {
			return cyI < cyJ
		}
		return sorted[i][0] < sorted[j][0]
	})
	var rows [][]maa.Rect
	cur := []maa.Rect{sorted[0]}
	for i := 1; i < len(sorted); i++ {
		if recordRectCenterY(sorted[i])-recordRectCenterY(sorted[i-1]) > recordRowClusterGapY {
			rows = append(rows, cur)
			cur = nil
		}
		cur = append(cur, sorted[i])
	}
	rows = append(rows, cur)
	for i := range rows {
		sort.Slice(rows[i], func(a, b int) bool {
			return rows[i][a][0] < rows[i][b][0]
		})
	}
	return rows
}

// recordOrderSlotBoxesByLayout 按平台骨架（Win32 7+3 / ADB 5+5）排序槽位锚框：上行从左到右，再下行从左到右。
func recordOrderSlotBoxesByLayout(boxes []maa.Rect, layout recordShelfLayout) []maa.Rect {
	rows := recordClusterRowsByYFromBoxes(boxes)
	if len(rows) == 0 {
		return nil
	}
	var top, bottom []maa.Rect
	switch len(rows) {
	case 1:
		top = rows[0]
	default:
		top = rows[0]
		bottom = rows[len(rows)-1]
	}
	want := layout.topRow + layout.bottomRow
	if len(top) != layout.topRow || len(bottom) != layout.bottomRow {
		log.Warn().
			Str("component", component).
			Int("top", len(top)).
			Int("bottom", len(bottom)).
			Int("want_top", layout.topRow).
			Int("want_bottom", layout.bottomRow).
			Msg("record: shelf slot row count differs from layout")
	}
	out := append(append([]maa.Rect(nil), top...), bottom...)
	if len(out) > recordMaxShelfSlots {
		log.Warn().
			Str("component", component).
			Int("slots", len(out)).
			Int("max", recordMaxShelfSlots).
			Msg("record: truncating extra shelf slot anchors")
		out = out[:recordMaxShelfSlots]
	}
	if len(out) != want && len(out) > 0 {
		log.Warn().
			Str("component", component).
			Int("slots", len(out)).
			Int("want", want).
			Msg("record: shelf slot anchor count mismatch")
	}
	return out
}

func recordAbsInt(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// recordHitBelongsToSlot 判定信用点锚点是否落在该格商品模板框内（720p：锚点在格下方，商品框覆盖整格）。
func recordHitBelongsToSlot(slot maa.Rect, hit itemPositionHit) bool {
	if !recordRectValid(slot) || !recordRectValid(hit.Box) {
		return false
	}
	scx, scy := recordRectCenter(slot)
	ib := hit.Box
	s := recordSlotMatchColSlack
	left := ib[0] - s
	top := ib[1] - s
	right := ib[0] + ib[2] + s
	bottom := ib[1] + ib[3] + s
	return scx >= left && scx <= right && scy >= top && scy <= bottom
}

type recordSlotItemPair struct {
	slotIdx int
	itemIdx int
	distSq  int
}

// recordMatchItemsToSlots 将商品模板命中挂到槽位锚框（每槽最多一件，每件最多占一槽）。
func recordMatchItemsToSlots(slotBoxes []maa.Rect, items []itemPositionHit) []itemPositionHit {
	out := make([]itemPositionHit, len(slotBoxes))
	if len(slotBoxes) == 0 || len(items) == 0 {
		return out
	}
	pairs := make([]recordSlotItemPair, 0, len(slotBoxes)*len(items))
	for si, slot := range slotBoxes {
		for ii, it := range items {
			if !recordHitBelongsToSlot(slot, it) {
				continue
			}
			scx, scy := recordRectCenter(slot)
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
