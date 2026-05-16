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
	Index       int    `json:"index"`
	NameRaw     string `json:"name_raw"`
	DiscountRaw string `json:"discount_raw"`
}

// ScanShelfSlots 流程：
//  1) 运行 RecordItemName，从 CombinedResult 中 ItemNameOCR 子节点取全部名称命中；
//  2) 将每条名称字框覆盖到 RecordItemDiscount 的 roi 锚点识别折扣；
//  3) 折扣未命中记为 None，与名称一并写入快照（见 storage / action_record）。
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
	for i, hit := range nameHits {
		out = append(out, SlotRecord{
			Index:       i,
			NameRaw:     hit.Text,
			DiscountRaw: recordDiscountAtNameBox(ctx, img, hit.Box),
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
