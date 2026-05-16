package creditshopping

import (
	"image"
	"strings"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const (
	pipelineNodeRecordItemName     = "RecordItemName"
	pipelineNodeRecordItemDiscount = "RecordItemDiscount"
	pipelineNodeItemNameOCR        = "ItemNameOCR"
	discountNone                   = "None"
)

type SlotRecord struct {
	ItemID   string `json:"item_id"`
	Discount string `json:"discount"`
}

// ScanShelfSlots 流程：
//  1) 运行 RecordItemName，从 CombinedResult 中 ItemNameOCR 子节点取全部名称命中；
//  2) OCR 文本经 item_map 匹配为 CreditShoppingItems 唯一 ID；
//  3) 将名称字框覆盖到 RecordItemDiscount 的 roi 锚点识别折扣（未命中记为 None）。
func ScanShelfSlots(ctx *maa.Context, img image.Image) []SlotRecord {
	nameDetail, err := ctx.RunRecognition(pipelineNodeRecordItemName, img, nil)
	if err != nil || nameDetail == nil || !nameDetail.Hit {
		log.Info().Str("component", component).Msg("shelf scan: no RecordItemName")
		return nil
	}

	nameHits := ocrNameHitsFromRecordItemName(nameDetail)
	if len(nameHits) == 0 {
		log.Warn().Str("component", component).Msg("shelf scan: RecordItemName hit but no ItemNameOCR results in CombinedResult")
		return nil
	}

	out := make([]SlotRecord, 0, len(nameHits))
	for _, hit := range nameHits {
		itemID, matched := matchCreditItemID(hit.Text)
		if !matched {
			log.Warn().
				Str("component", component).
				Str("ocr_text", hit.Text).
				Msg("shelf scan: unmatched item name, skip slot")
			continue
		}
		out = append(out, SlotRecord{
			ItemID:   itemID,
			Discount: recordDiscountAtNameBox(ctx, img, hit.Box),
		})
	}
	return out
}

func recordDiscountAtNameBox(ctx *maa.Context, img image.Image, nameBox maa.Rect) string {
	override := map[string]any{
		pipelineNodeItemNameOCR: map[string]any{
			"roi": nameBox,
		},
	}
	detail, err := ctx.RunRecognition(pipelineNodeRecordItemDiscount, img, override)
	if err != nil || detail == nil || !detail.Hit {
		return discountNone
	}
	text := strings.TrimSpace(bestOCRText(detail))
	if text == "" {
		return discountNone
	}
	return text
}
