package creditshopping

import (
	"image"
	"strings"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/iconrecognition"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const (
	pipelineNodeRecordShelfSlot     = "CreditShoppingRecordShelfSlot"
	pipelineNodeRecordCatalogItems  = "CreditShoppingRecordCatalogItems"
	pipelineNodeRecordItemDiscount  = "RecordItemDiscount"
	recordDiscountNone              = "None"
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

func recordCatalogItemByCaseID(caseID string) (recordCatalogItem, bool) {
	for _, item := range recordCatalog {
		if item.ID == caseID {
			return item, true
		}
	}
	return recordCatalogItem{}, false
}

// recordCollectCatalogHits 对 CreditShoppingRecordCatalogItems 跑 IconRecognition 并汇总命中。
func recordCollectCatalogHits(ctx *maa.Context, img image.Image) []itemPositionHit {
	detail, err := ctx.RunRecognition(pipelineNodeRecordCatalogItems, img, nil)
	if err != nil || detail == nil || !detail.Hit {
		return nil
	}
	parsed, err := iconrecognition.NewRecognitionDetail(detail)
	if err != nil {
		log.Warn().Err(err).Str("component", component).Msg("record: parse IconRecognition detail failed")
		return nil
	}
	matches, err := iconrecognition.CollectMatches(parsed.All(), parsed.Filter(), parsed.Best())
	if err != nil {
		log.Warn().Err(err).Str("component", component).Msg("record: IconRecognition matches failed")
		return nil
	}
	hits := make([]itemPositionHit, 0, len(matches))
	for _, match := range matches {
		if !recordRectValid(match.CellBox) {
			continue
		}
		caseID, ok := creditCaseIDFromRecognitionItemID(match.ItemID)
		if !ok {
			continue
		}
		catalog, ok := recordCatalogItemByCaseID(caseID)
		if !ok {
			continue
		}
		hits = append(hits, itemPositionHit{
			Box:  match.CellBox,
			Name: catalog.Name,
			ID:   catalog.ID,
		})
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

// RecordShelfFromImage 槽位骨架（CreditShoppingRecordShelfSlot）+ IconRecognition 挂格 + 折扣 OCR（不写盘）。
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
