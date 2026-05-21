package creditshopping

import (
	"testing"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

func TestApplyROIOffset(t *testing.T) {
	t.Parallel()
	base := maa.Rect{100, 200, 80, 24}
	offset := recordItemDiscountROIOffsetPC
	got := applyROIOffset(base, offset)
	want := maa.Rect{
		base[0] + offset[0],
		base[1] + offset[1],
		base[2] + offset[2],
		base[3] + offset[3],
	}
	if got != want {
		t.Fatalf("applyROIOffset() = %v, want %v", got, want)
	}
}

func TestRecordItemDiscountPipelineOverride(t *testing.T) {
	t.Parallel()
	nameBox := maa.Rect{100, 200, 80, 24}
	override := recordItemDiscountPipelineOverride(nameBox, nil)
	node, ok := override[pipelineNodeRecordItemDiscount].(map[string]any)
	if !ok {
		t.Fatalf("override node type = %T", override[pipelineNodeRecordItemDiscount])
	}
	if node["roi"] != nameBox {
		t.Fatalf("roi = %v, want %v", node["roi"], nameBox)
	}
	off, ok := node["roi_offset"].([]int)
	if !ok {
		t.Fatalf("roi_offset type = %T", node["roi_offset"])
	}
	want := []int{44, -170, 4, 4}
	if len(off) != len(want) {
		t.Fatalf("roi_offset = %v", off)
	}
	for i := range want {
		if off[i] != want[i] {
			t.Fatalf("roi_offset[%d] = %d, want %d", i, off[i], want[i])
		}
	}
	// 由 pipeline 执行一次偏移，不得在 Go 侧预叠加到 roi。
	if applyROIOffset(nameBox, recordItemDiscountROIOffsetPC) == node["roi"] {
		t.Fatal("roi must be nameBox, not pre-offset discount ROI")
	}
}
