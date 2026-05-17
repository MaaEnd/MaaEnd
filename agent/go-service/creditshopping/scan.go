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
	pipelineNodeRecordSellOut      = "RecordSellOut"
	pipelineNodeItemNameOCR        = "ItemNameOCR"
	discountNone                   = "None"
)

type SlotRecord struct {
	Slot     int    `json:"slot"`
	ItemID   string `json:"item_id"`
	Discount string `json:"discount"`
	// Buy 是否已购买：true=售罄（用户已买），false=未售罄（仍可购买）。
	Buy bool `json:"buy"`
}

func scanShelfNameHits(ctx *maa.Context, img image.Image) []ocrNameHit {
	nameDetail, err := ctx.RunRecognition(pipelineNodeRecordItemName, img, nil)
	if err != nil || nameDetail == nil || !nameDetail.Hit {
		log.Info().Str("component", component).Msg("shelf scan: no RecordItemName")
		return nil
	}
	hits := ocrNameHitsFromRecordItemName(nameDetail)
	if len(hits) == 0 {
		log.Warn().Str("component", component).Msg("shelf scan: RecordItemName hit but no ItemNameOCR results")
	}
	return hits
}

// ScanShelfSlotsPC 单次截图：按 Y 聚类为两排，上 7 槽（0–6）、下 3 槽（7–9），按 X 排序。
func ScanShelfSlotsPC(ctx *maa.Context, img image.Image) []SlotRecord {
	hits := scanShelfNameHits(ctx, img)
	return buildSlotRecords(ctx, img, hits, slotAssignPC)
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

// recordBuyAtNameBox 运行 RecordSellOut：未命中（售罄）表示用户已购买该槽位商品。
func recordBuyAtNameBox(ctx *maa.Context, img image.Image, nameBox maa.Rect) bool {
	override := map[string]any{
		pipelineNodeItemNameOCR: map[string]any{
			"roi": nameBox,
		},
	}
	detail, err := ctx.RunRecognition(pipelineNodeRecordSellOut, img, override)
	if err != nil || detail == nil {
		return false
	}
	return !detail.Hit
}
