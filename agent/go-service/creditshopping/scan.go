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
	Slot     int    `json:"slot"`
	Name     string `json:"name"`
	ID       string `json:"id,omitempty"`
	Discount string `json:"discount"`
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
	var ctrl *maa.Controller
	if ctx != nil && ctx.GetTasker() != nil {
		ctrl = ctx.GetTasker().GetController()
	}
	// 覆写 roi 为当前槽位名称框，并显式指定 roi_offset（与 pipeline 一致）。
	// 不可先 applyROIOffset 再只覆写 roi：流水线仍会再叠一层 roi_offset。
	override := recordItemDiscountPipelineOverride(nameBox, ctrl)
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
