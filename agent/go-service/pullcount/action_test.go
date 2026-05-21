package pullcount

import (
	"image"
	"image/color"
	"testing"
)

// TestCalculatePullCount verifies the recruitment screen resource formula from issue #2147.
func TestCalculatePullCount(t *testing.T) {
	param, err := parseActionParam("")
	if err != nil {
		t.Fatalf("parseActionParam() error = %v", err)
	}

	summary := voucherSummary{
		CurrentOnlyPulls: 2,
		CarryToNextPulls: 3,
		NextOnlyPulls:    10,
	}
	got := calculatePullCount(resourceValues{
		Originium: 2925,
		Oroberyl:  20770,
	}, summary, param)

	if got.ReservedOriginiumValue != 2175 {
		t.Fatalf("reserved originium value = %d, want 2175", got.ReservedOriginiumValue)
	}
	if got.UsableOriginium != 750 {
		t.Fatalf("usable originium = %d, want 750", got.UsableOriginium)
	}
	if got.ResourcePulls != 43 {
		t.Fatalf("resource pulls = %d, want 43", got.ResourcePulls)
	}
	if got.CurrentPoolTotal != 48 {
		t.Fatalf("current pool total = %d, want 48", got.CurrentPoolTotal)
	}
	if got.NextPoolTotal != 66 {
		t.Fatalf("next pool total = %d, want 66", got.NextPoolTotal)
	}
}

// TestCalculatePullCountClampsReservedOriginium verifies reserved originium never goes negative.
func TestCalculatePullCountClampsReservedOriginium(t *testing.T) {
	param, err := parseActionParam("")
	if err != nil {
		t.Fatalf("parseActionParam() error = %v", err)
	}

	got := calculatePullCount(resourceValues{
		Originium: 2000,
		Oroberyl:  499,
	}, voucherSummary{}, param)

	if got.UsableOriginium != 0 {
		t.Fatalf("usable originium = %d, want 0", got.UsableOriginium)
	}
	if got.ResourcePulls != 0 {
		t.Fatalf("resource pulls = %d, want 0", got.ResourcePulls)
	}
	if got.NextPoolTotal != 10 {
		t.Fatalf("next pool total = %d, want fixed 10", got.NextPoolTotal)
	}
}

// TestSummarizeVouchers verifies voucher weights and pool scopes.
func TestSummarizeVouchers(t *testing.T) {
	config := &voucherConfig{Vouchers: []voucherDef{
		{Name: "当前单抽券", PullValue: 1, PoolScope: "current_only"},
		{Name: "通用单抽券", PullValue: 1, PoolScope: "carry_to_next"},
		{Name: "下池十连券", PullValue: 10, PoolScope: "next_only"},
	}}

	got := summarizeVouchers([]scannedVoucher{
		{Name: "当前单抽券", Quantity: 2},
		{Name: "通用单抽券", Quantity: 3},
		{Name: "下池十连券", Quantity: 1},
		{Name: "未配置寻访凭证", Quantity: 7},
		{Name: "无关物品", Quantity: 99},
	}, config)

	if got.CurrentOnlyPulls != 2 {
		t.Fatalf("current only pulls = %d, want 2", got.CurrentOnlyPulls)
	}
	if got.CarryToNextPulls != 3 {
		t.Fatalf("carry to next pulls = %d, want 3", got.CarryToNextPulls)
	}
	if got.NextOnlyPulls != 10 {
		t.Fatalf("next only pulls = %d, want 10", got.NextOnlyPulls)
	}
	if len(got.UnknownNames) != 1 || got.UnknownNames[0] != "未配置寻访凭证" {
		t.Fatalf("unknown names = %#v, want [未配置寻访凭证]", got.UnknownNames)
	}
}

// TestParseIntegerText accepts compact OCR noise around a counter.
func TestParseIntegerText(t *testing.T) {
	cases := map[string]int{
		" 20,770 |": 20770,
		"20770 1":   20770,
	}

	for text, want := range cases {
		got, err := parseIntegerText(text)
		if err != nil {
			t.Fatalf("parseIntegerText(%q) error = %v", text, err)
		}
		if got != want {
			t.Fatalf("parseIntegerText(%q) = %d, want %d", text, got, want)
		}
	}
}

// TestScannedPageSignatureUsesAllReadableItems verifies non-voucher items can stop repeated page scans.
func TestScannedPageSignatureUsesAllReadableItems(t *testing.T) {
	first := []scannedVoucher{
		{Name: "普通物品", Quantity: 3},
		{Name: "无关材料", Quantity: 12},
	}
	second := []scannedVoucher{
		{Name: "无关材料", Quantity: 12},
		{Name: "普通物品", Quantity: 3},
	}

	firstSig := scannedPageSignature(first)
	secondSig := scannedPageSignature(second)
	if firstSig == "" {
		t.Fatalf("scannedPageSignature() returned empty signature for readable non-voucher items")
	}
	if firstSig != secondSig {
		t.Fatalf("scannedPageSignature() = %q, want order-independent %q", secondSig, firstSig)
	}
}

// TestWarehouseGridChangedDetectsUnchangedGrid verifies identical scroll ROI means the list reached bottom.
func TestWarehouseGridChangedDetectsUnchangedGrid(t *testing.T) {
	before := solidImage(1280, 720, color.RGBA{R: 20, G: 30, B: 40, A: 255})
	after := solidImage(1280, 720, color.RGBA{R: 20, G: 30, B: 40, A: 255})

	changed, ratio := warehouseGridChanged(before, after, roiParam{X: 40, Y: 120, W: 930, H: 540}, 0.01)
	if changed {
		t.Fatalf("warehouseGridChanged() changed = true, want false; ratio=%f", ratio)
	}
	if ratio != 0 {
		t.Fatalf("warehouseGridChanged() ratio = %f, want 0", ratio)
	}
}

// TestWarehouseGridChangedDetectsChangedGrid verifies movement inside the grid ROI exceeds the threshold.
func TestWarehouseGridChangedDetectsChangedGrid(t *testing.T) {
	before := solidImage(1280, 720, color.RGBA{R: 20, G: 30, B: 40, A: 255})
	after := solidImage(1280, 720, color.RGBA{R: 20, G: 30, B: 40, A: 255})
	fillRect(after, image.Rect(60, 140, 260, 340), color.RGBA{R: 220, G: 220, B: 220, A: 255})

	changed, ratio := warehouseGridChanged(before, after, roiParam{X: 40, Y: 120, W: 930, H: 540}, 0.01)
	if !changed {
		t.Fatalf("warehouseGridChanged() changed = false, want true; ratio=%f", ratio)
	}
}

// solidImage creates a filled RGBA image for image-diff tests.
func solidImage(width int, height int, c color.Color) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	fillRect(img, img.Bounds(), c)
	return img
}

// fillRect fills an image rectangle with one color.
func fillRect(img *image.RGBA, rect image.Rectangle, c color.Color) {
	for y := rect.Min.Y; y < rect.Max.Y; y++ {
		for x := rect.Min.X; x < rect.Max.X; x++ {
			img.Set(x, y, c)
		}
	}
}
