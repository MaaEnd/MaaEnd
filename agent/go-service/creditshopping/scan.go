package creditshopping

import (
	"image"
	"strings"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const (
	pipelineNodeRecordItemName      = "RecordItemName"
	pipelineNodeItemNameOCRExpected = "ItemNameOCR_Expected"
	pipelineNodeRecordItemDiscount  = "RecordItemDiscount"
	pipelineNodeItemNameOCR         = "ItemNameOCR"
	discountNone                    = "None"
)

type SlotRecord struct {
	Index       int    `json:"index"`
	NameRaw     string `json:"name_raw"`
	DiscountRaw string `json:"discount_raw"`
}

// ScanShelfSlots 流程：
//  1) 运行 RecordItemName 获取各槽位物品名称框；
//  2) 遍历 ItemNameOCR_Expected 的全部命中框，将每条 box 作为 RecordItemDiscount 的 roi 锚点执行识别；
//  3) 折扣未命中记为 None，与名称一并写入快照（见 storage / action_record）。
func ScanShelfSlots(ctx *maa.Context, img image.Image) []SlotRecord {
	nameDetail, err := ctx.RunRecognition(pipelineNodeRecordItemName, img, nil)
	if err != nil || nameDetail == nil || !nameDetail.Hit {
		log.Info().Str("component", component).Msg("shelf scan: no RecordItemName")
		return nil
	}

	expectedDetail, err := ctx.RunRecognition(pipelineNodeItemNameOCRExpected, img, nil)
	if err != nil || expectedDetail == nil || !expectedDetail.Hit {
		log.Info().Str("component", component).Msg("shelf scan: no ItemNameOCR_Expected")
		return nil
	}

	nameBoxes := recognitionBoxes(nameDetail)
	sortRectsVertical(nameBoxes)

	discountAnchors := recognitionBoxes(expectedDetail)
	sortRectsVertical(discountAnchors)
	if len(discountAnchors) == 0 {
		return nil
	}
	if len(nameBoxes) != len(discountAnchors) {
		log.Warn().
			Str("component", component).
			Int("record_item_name", len(nameBoxes)).
			Int("item_name_expected", len(discountAnchors)).
			Msg("shelf scan: name hit count mismatch, zip by index")
	}

	out := make([]SlotRecord, 0, len(discountAnchors))
	for i, anchor := range discountAnchors {
		rec := SlotRecord{
			Index:       i,
			DiscountRaw: recordDiscountAtNameBox(ctx, img, anchor),
		}
		if i < len(nameBoxes) {
			rec.NameRaw = pipelineOCRTextAtROI(ctx, img, pipelineNodeItemNameOCR, nameBoxes[i])
		}
		out = append(out, rec)
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

func pipelineOCRTextAtROI(ctx *maa.Context, img image.Image, node string, roi maa.Rect) string {
	override := map[string]any{
		node: map[string]any{
			"roi": roi,
		},
	}
	detail, err := ctx.RunRecognition(node, img, override)
	if err != nil || detail == nil || !detail.Hit {
		return ""
	}
	return strings.TrimSpace(bestOCRText(detail))
}
