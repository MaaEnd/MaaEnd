package creditshopping

import (
	"image"
	"strings"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const (
	pipelineNodeRecordShelfSlot    = "CreditShoppingRecordShelfSlot"
	pipelineNodeRecordItemDiscount = "RecordItemDiscount"
	recordDiscountNone             = "None"
)

func recordRectValid(r maa.Rect) bool {
	return r[2] > 0 && r[3] > 0
}

func recordDiscountPipelineOverride(anchorBox maa.Rect) map[string]any {
	return map[string]any{
		pipelineNodeRecordItemDiscount: map[string]any{
			"roi": anchorBox,
		},
	}
}

func recordFindShelfSlotBoxes(ctx *maa.Context, img image.Image, layout recordShelfLayout) []maa.Rect {
	detail, err := ctx.RunRecognition(pipelineNodeRecordShelfSlot, img, nil)
	if err != nil || detail == nil || !detail.Hit {
		log.Info().Str("component", component).Msg("record: shelf slot anchor miss")
		return nil
	}
	boxes := make([]maa.Rect, 0, recordMaxShelfSlots)
	for _, result := range recognitionResults(detail) {
		matched, ok := result.AsTemplateMatch()
		if !ok || matched == nil || !recordRectValid(matched.Box) {
			continue
		}
		boxes = append(boxes, matched.Box)
	}
	if len(boxes) == 0 {
		log.Info().Str("component", component).Msg("record: shelf slot anchor hit but no boxes")
		return nil
	}
	return recordOrderSlotBoxesByLayout(boxes, layout)
}

// recordCollectCatalogHits 对 record.json 各 CreditShoppingRecordItem* 节点跑 Pipeline 模板识别并汇总命中。
func recordCollectCatalogHits(ctx *maa.Context, img image.Image) []itemPositionHit {
	hits := make([]itemPositionHit, 0, recordMaxShelfSlots)
	for _, item := range recordCatalog {
		detail, err := ctx.RunRecognition(item.Node, img, nil)
		if err != nil || detail == nil || !detail.Hit {
			continue
		}
		for _, result := range recognitionResults(detail) {
			matched, ok := result.AsTemplateMatch()
			if !ok || matched == nil || !recordRectValid(matched.Box) {
				continue
			}
			hits = append(hits, itemPositionHit{
				Box:  matched.Box,
				Name: item.Name,
				ID:   item.ID,
			})
		}
	}
	return hits
}

func recordRecognizeDiscountAt(ctx *maa.Context, img image.Image, anchorBox maa.Rect) string {
	override := recordDiscountPipelineOverride(anchorBox)
	detail, err := ctx.RunRecognition(pipelineNodeRecordItemDiscount, img, override)
	if err != nil || detail == nil || !detail.Hit {
		return recordDiscountNone
	}
	text := strings.TrimSpace(bestOCRText(detail))
	if text == "" {
		return recordDiscountNone
	}
	return text
}

func recordAssembleSlotRecords(
	slotBoxes []maa.Rect,
	matched []itemPositionHit,
	discountFor func(anchor maa.Rect) string,
) []SlotRecord {
	out := make([]SlotRecord, 0, len(slotBoxes))
	for i, slotBox := range slotBoxes {
		hit := matched[i]
		discountBox := slotBox
		name := recordSlotNameUnknown
		id := recordSlotItemIDUnknown
		if recordRectValid(hit.Box) {
			discountBox = hit.Box
			name = strings.TrimSpace(hit.Name)
			id = hit.ID
		}
		out = append(out, SlotRecord{
			Slot:     i,
			Name:     name,
			ID:       id,
			Discount: discountFor(discountBox),
		})
	}
	return out
}

// RecordShelfFromImage 槽位骨架（CreditShoppingRecordShelfSlot）+ record 物品模板挂格 + 折扣 OCR（不写盘）。
func RecordShelfFromImage(ctx *maa.Context, img image.Image, layout recordShelfLayout) []SlotRecord {
	slotBoxes := recordFindShelfSlotBoxes(ctx, img, layout)
	if len(slotBoxes) == 0 {
		return nil
	}
	itemHits := recordCollectCatalogHits(ctx, img)
	matched := recordMatchItemsToSlots(slotBoxes, itemHits)
	return recordAssembleSlotRecords(slotBoxes, matched, func(anchor maa.Rect) string {
		return recordRecognizeDiscountAt(ctx, img, anchor)
	})
}
