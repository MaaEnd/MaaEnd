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
