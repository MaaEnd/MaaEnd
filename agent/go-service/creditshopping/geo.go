package creditshopping

import maa "github.com/MaaXYZ/maa-framework-go/v4"

const component = "creditshopping"

// 折扣 OCR 相对物品模板框，与未售罄到折扣的偏移一致。
var recordItemDiscountROIOffset = maa.Rect{100, 2, -104, -140}

func recordItemDiscountPipelineOverride(nameBox maa.Rect) map[string]any {
	off := recordItemDiscountROIOffset
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
