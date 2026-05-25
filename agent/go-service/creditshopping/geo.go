package creditshopping

import maa "github.com/MaaXYZ/maa-framework-go/v4"

const component = "creditshopping"

// 与 assets/resource/pipeline/CreditShopping/record.json、
// assets/resource_adb/pipeline/CreditShopping/record.json 中 RecordItemDiscount.roi_offset 保持一致。
var (
	recordItemDiscountROIOffsetPC  = maa.Rect{44, -170, 4, 4}
	recordItemDiscountROIOffsetADB = maa.Rect{62, -213, -6, 7}
)

func recordItemDiscountROIOffset(ctrl *maa.Controller) maa.Rect {
	if isADBController(ctrl) {
		return recordItemDiscountROIOffsetADB
	}
	return recordItemDiscountROIOffsetPC
}

// recordItemDiscountPipelineOverride 为单槽折扣 OCR 构造 pipeline override。
func recordItemDiscountPipelineOverride(nameBox maa.Rect, ctrl *maa.Controller) map[string]any {
	off := recordItemDiscountROIOffset(ctrl)
	return map[string]any{
		pipelineNodeRecordItemDiscount: map[string]any{
			"roi":        nameBox,
			"roi_offset": []int{off[0], off[1], off[2], off[3]},
		},
	}
}

// applyROIOffset 与 Pipeline 协议 roi_offset 语义一致：在 base 矩形四元组上分别相加。
func applyROIOffset(base, offset maa.Rect) maa.Rect {
	return maa.Rect{
		base[0] + offset[0],
		base[1] + offset[1],
		base[2] + offset[2],
		base[3] + offset[3],
	}
}

func rectValid(r maa.Rect) bool {
	return r[2] > 0 && r[3] > 0
}

func targetRect(r maa.Rect) maa.Target {
	return maa.NewTargetRect(r)
}
