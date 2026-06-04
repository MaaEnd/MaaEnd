package minicv

import (
	"image"
	"image/color"
	"math"
	"testing"
)

func cropAsTemplate(src *image.RGBA, x, y, w, h int) *image.RGBA {
	tpl := image.NewRGBA(image.Rect(0, 0, w, h))
	for row := range h {
		srcOff := (y+row)*src.Stride + x*4
		dstOff := row * tpl.Stride
		copy(tpl.Pix[dstOff:dstOff+w*4], src.Pix[srcOff:srcOff+w*4])
	}
	return tpl
}

func generateMatchTestImage(w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		off := y * img.Stride
		for x := range w {
			r := uint8((x*x*3 + y*17 + (x*y)%251) & 0xFF)
			g := uint8((x*29 + y*y*5 + (x^y)*7) & 0xFF)
			b := uint8((((x * 11) ^ (y * 23)) + x*y*13) & 0xFF)
			img.Pix[off] = r
			img.Pix[off+1] = g
			img.Pix[off+2] = b
			img.Pix[off+3] = 255
			off += 4
		}
	}

	ImageDrawLine(img, 37, 29, 1217, 681, color.RGBA{R: 255, G: 64, B: 32, A: 255}, 3)
	ImageDrawLine(img, 120, 690, 1260, 18, color.RGBA{R: 32, G: 220, B: 255, A: 255}, 2)
	ImageDrawFilledCircle(img, 640, 360, 41, color.RGBA{R: 250, G: 220, B: 40, A: 255})
	ImageDrawFilledCircle(img, 72, 76, 23, color.RGBA{R: 40, G: 240, B: 110, A: 255})
	ImageDrawFilledCircle(img, 1196, 644, 19, color.RGBA{R: 220, G: 60, B: 220, A: 255})

	return img
}

func decorateTargetArea(img *image.RGBA, x, y, w, h int) {
	cx := x + w/2
	cy := y + h/2

	ImageDrawLine(img, x+3, y+5, x+w-4, y+h-6, color.RGBA{R: 255, G: 16, B: 16, A: 255}, 3)
	ImageDrawLine(img, x+w-5, y+4, x+4, y+h-5, color.RGBA{R: 16, G: 255, B: 48, A: 255}, 2)
	ImageDrawLine(img, x+w/2, y+2, x+w/2, y+h-3, color.RGBA{R: 32, G: 96, B: 255, A: 255}, 2)
	ImageDrawFilledCircle(img, cx, cy, min(w, h)/5, color.RGBA{R: 255, G: 220, B: 0, A: 255})
	ImageDrawFilledCircle(img, x+w/3, y+h/3, max(3, min(w, h)/10), color.RGBA{R: 0, G: 220, B: 255, A: 255})
}

func decorateMaskedTargetArea(img *image.RGBA, x, y, w, h int) {
	decorateTargetArea(img, x, y, w, h)
	ImageDrawFilledCircle(img, x+w/3, y+h*3/4, max(3, min(w, h)/12), color.RGBA{R: 255, G: 48, B: 180, A: 255})
	ImageDrawFilledCircle(img, x+w*3/4, y+h/3, max(3, min(w, h)/12), color.RGBA{R: 48, G: 255, B: 180, A: 255})
}

func maskTemplateOuterRing(tpl *image.RGBA) {
	w, h := tpl.Rect.Dx(), tpl.Rect.Dy()
	maskColor := color.RGBA{R: 0, G: 255, B: 0, A: 255}
	thickX := max(1, w*15/100)
	thickY := max(1, h*15/100)
	diagonalHalfThickness := float64(w+h) * 0.05 / 4.0
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			onOuterRing := x < thickX || x >= w-thickX || y < thickY || y >= h-thickY
			onDiagonal := distanceToMainDiagonal(x, y, w, h) <= diagonalHalfThickness
			if onOuterRing || onDiagonal {
				tpl.SetRGBA(x, y, maskColor)
			}
		}
	}
}

func distanceToMainDiagonal(x, y, w, h int) float64 {
	if w <= 1 || h <= 1 {
		return 0
	}

	fx := float64(x)
	fy := float64(y)
	fw := float64(w - 1)
	fh := float64(h - 1)
	return math.Abs(fh*fx-fw*fy) / math.Hypot(fw, fh)
}

func assertMatchNear(t *testing.T, gotX, gotY float64, wantX, wantY int) {
	t.Helper()
	if math.Abs(gotX-float64(wantX)) > 0.1 || math.Abs(gotY-float64(wantY)) > 0.1 {
		t.Fatalf("unexpected match position: got=(%.3f, %.3f), want=(%d, %d)", gotX, gotY, wantX, wantY)
	}
}

func assertZeroMatch(t *testing.T, gotX, gotY, gotScore float64) {
	t.Helper()
	if gotX != 0 || gotY != 0 || gotScore != 0 {
		t.Fatalf("unexpected non-zero match result: got=(%.3f, %.3f, %.3f), want=(0, 0, 0)", gotX, gotY, gotScore)
	}
}

func TestMatchTemplateNilInputs(t *testing.T) {
	img := generateMatchTestImage(128, 128)
	imgIntArr := GetIntegralArray(img)
	tpl := cropAsTemplate(img, 16, 16, 32, 32)
	tplStats := GetImageStats(tpl)

	testCases := []struct {
		name string
		run  func() (float64, float64, float64)
	}{
		{
			name: "whole_image_nil_img",
			run: func() (float64, float64, float64) {
				return MatchTemplate(nil, imgIntArr, tpl, tplStats)
			},
		},
		{
			name: "whole_image_nil_tpl",
			run: func() (float64, float64, float64) {
				return MatchTemplate(img, imgIntArr, nil, tplStats)
			},
		},
		{
			name: "in_area_nil_img",
			run: func() (float64, float64, float64) {
				return MatchTemplateInArea(nil, imgIntArr, tpl, tplStats, [4]int{0, 0, 128, 128})
			},
		},
		{
			name: "in_area_nil_tpl",
			run: func() (float64, float64, float64) {
				return MatchTemplateInArea(img, imgIntArr, nil, tplStats, [4]int{0, 0, 128, 128})
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			x, y, score := tc.run()
			assertZeroMatch(t, x, y, score)
		})
	}
}

func TestMatchTemplate(t *testing.T) {
	testCases := []struct {
		name string
		tplX int
		tplY int
		tplW int
		tplH int
	}{
		{name: "center", tplX: 608, tplY: 328, tplW: 64, tplH: 64},
		{name: "near_edge", tplX: 9, tplY: 14, tplW: 64, tplH: 64},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			img := generateMatchTestImage(1280, 720)
			decorateTargetArea(img, tc.tplX, tc.tplY, tc.tplW, tc.tplH)
			imgIntArr := GetIntegralArray(img)
			tpl := cropAsTemplate(img, tc.tplX, tc.tplY, tc.tplW, tc.tplH)
			tplStats := GetImageStats(tpl)

			x, y, score := MatchTemplate(img, imgIntArr, tpl, tplStats)

			assertMatchNear(t, x, y, tc.tplX, tc.tplY)
			if score < 0.9999 {
				t.Fatalf("unexpected match score: got=%.6f, want>=0.9999", score)
			}
		})
	}
}

func TestMatchTemplateWithMask(t *testing.T) {
	img := generateMatchTestImage(512, 384)
	decorateMaskedTargetArea(img, 81, 68, 64, 48)
	imgIntArr := GetIntegralArray(img)
	tpl := cropAsTemplate(img, 81, 68, 64, 48)
	maskTemplateOuterRing(tpl)
	tplStats := GetImageStats(tpl)

	x, y, score := MatchTemplateWithMask(img, imgIntArr, tpl, tplStats, 0x00FF00)

	assertMatchNear(t, x, y, 81, 68)
	if score < 0.9999 {
		t.Fatalf("unexpected match score: got=%.6f, want>=0.9999", score)
	}
}

func TestMatchTemplateMultiHitWithMask(t *testing.T) {
	img := generateMatchTestImage(512, 384)
	decorateMaskedTargetArea(img, 81, 68, 64, 48)
	for row := range 48 {
		srcOff := (68+row)*img.Stride + 81*4
		dstOff := (244+row)*img.Stride + 321*4
		copy(img.Pix[dstOff:dstOff+64*4], img.Pix[srcOff:srcOff+64*4])
	}
	imgIntArr := GetIntegralArray(img)
	tpl := cropAsTemplate(img, 81, 68, 64, 48)
	maskTemplateOuterRing(tpl)
	tplStats := GetImageStats(tpl)

	hits := MatchTemplateMultiHitWithMask(img, imgIntArr, tpl, tplStats, 0x00FF00, 0.9999, 4)
	if len(hits) != 2 {
		t.Fatalf("unexpected hit count: got=%d, want=2", len(hits))
	}

	assertMatchNear(t, hits[0].X, hits[0].Y, 81, 68)
	assertMatchNear(t, hits[1].X, hits[1].Y, 321, 244)
}

func TestBuildCircleMaskTemplate(t *testing.T) {
	tpl := cropAsTemplate(generateMatchTestImage(64, 64), 8, 8, 32, 32)
	circleTpl := BuildCircleMaskTemplate(tpl, Circle{X: 16, Y: 16, Radius: 8}, 0x00FF00)

	if got := circleTpl.RGBAAt(16, 16); got != tpl.RGBAAt(16, 16) {
		t.Fatalf("unexpected center pixel: got=%v, want=%v", got, tpl.RGBAAt(16, 16))
	}
	if got := circleTpl.RGBAAt(0, 0); got.R != 0 || got.G != 255 || got.B != 0 {
		t.Fatalf("unexpected masked pixel: got=%v, want RGB=(0,255,0)", got)
	}
}

func BenchmarkMatchTemplate(b *testing.B) {
	img := generateBenchmarkImage(1280, 720)
	imgIntArr := GetIntegralArray(img)

	benchmarks := []struct {
		name string
		tplW int
		tplH int
		tplX int
		tplY int
	}{
		{name: "tpl_32x32", tplW: 32, tplH: 32, tplX: 160, tplY: 120},
		{name: "tpl_64x64", tplW: 64, tplH: 64, tplX: 480, tplY: 240},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			tpl := cropAsTemplate(img, bm.tplX, bm.tplY, bm.tplW, bm.tplH)
			tplStats := GetImageStats(tpl)

			b.ReportAllocs()
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				_, _, _ = MatchTemplate(img, imgIntArr, tpl, tplStats)
			}
		})
	}
}

func BenchmarkMatchTemplateWithMask(b *testing.B) {
	img := generateBenchmarkImage(1280, 720)
	imgIntArr := GetIntegralArray(img)

	benchmarks := []struct {
		name string
		tplW int
		tplH int
		tplX int
		tplY int
	}{
		{name: "tpl_32x32", tplW: 32, tplH: 32, tplX: 160, tplY: 120},
		{name: "tpl_64x64", tplW: 64, tplH: 64, tplX: 480, tplY: 240},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			tpl := cropAsTemplate(img, bm.tplX, bm.tplY, bm.tplW, bm.tplH)
			maskTemplateOuterRing(tpl)
			tplStats := GetImageStats(tpl)

			b.ReportAllocs()
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				_, _, _ = MatchTemplateWithMask(img, imgIntArr, tpl, tplStats, 0x00FF00)
			}
		})
	}
}
