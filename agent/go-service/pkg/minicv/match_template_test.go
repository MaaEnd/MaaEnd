// Copyright (c) 2026 Harry Huang
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

func TestMatchCircleTemplateNilInputs(t *testing.T) {
	img := generateMatchTestImage(128, 128)
	imgIntArr := GetIntegralArray(img)
	tpl := cropAsTemplate(img, 16, 16, 48, 48)
	polar := Circle{X: 24, Y: 24, Radius: 12}
	tplCircleStats := GetImageCircleStats(tpl, polar)

	testCases := []struct {
		name string
		run  func() (float64, float64, float64)
	}{
		{
			name: "whole_image_nil_img",
			run: func() (float64, float64, float64) {
				return MatchCircleTemplate(nil, imgIntArr, tpl, tplCircleStats, polar)
			},
		},
		{
			name: "whole_image_nil_tpl",
			run: func() (float64, float64, float64) {
				return MatchCircleTemplate(img, imgIntArr, nil, tplCircleStats, polar)
			},
		},
		{
			name: "in_area_nil_img",
			run: func() (float64, float64, float64) {
				return MatchCircleTemplateInArea(nil, imgIntArr, tpl, tplCircleStats, polar, [4]int{0, 0, 128, 128})
			},
		},
		{
			name: "in_area_nil_tpl",
			run: func() (float64, float64, float64) {
				return MatchCircleTemplateInArea(img, imgIntArr, nil, tplCircleStats, polar, [4]int{0, 0, 128, 128})
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

func TestMatchCircleTemplate(t *testing.T) {
	testCases := []struct {
		name        string
		tplX        int
		tplY        int
		tplW        int
		tplH        int
		polarRadius int
	}{
		{name: "center", tplX: 592, tplY: 312, tplW: 96, tplH: 96, polarRadius: 23},
		{name: "near_edge", tplX: 12, tplY: 18, tplW: 96, tplH: 96, polarRadius: 23},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			img := generateMatchTestImage(1280, 720)
			decorateTargetArea(img, tc.tplX, tc.tplY, tc.tplW, tc.tplH)
			imgIntArr := GetIntegralArray(img)
			tpl := cropAsTemplate(img, tc.tplX, tc.tplY, tc.tplW, tc.tplH)
			polar := Circle{X: tc.tplW / 2, Y: tc.tplH / 2, Radius: tc.polarRadius}
			tplCircleStats := GetImageCircleStats(tpl, polar)

			x, y, score := MatchCircleTemplate(img, imgIntArr, tpl, tplCircleStats, polar)

			assertMatchNear(t, x, y, tc.tplX, tc.tplY)
			if score < 0.9999 {
				t.Fatalf("unexpected circle match score: got=%.6f, want>=0.9999", score)
			}
		})
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

func BenchmarkMatchCircleTemplate(b *testing.B) {
	img := generateBenchmarkImage(1280, 720)
	imgIntArr := GetIntegralArray(img)

	benchmarks := []struct {
		name        string
		tplW        int
		tplH        int
		tplX        int
		tplY        int
		polarRadius int
	}{
		{
			name:        "r23",
			tplW:        128,
			tplH:        128,
			tplX:        420,
			tplY:        260,
			polarRadius: 23,
		},
		{
			name:        "r45",
			tplW:        128,
			tplH:        128,
			tplX:        420,
			tplY:        260,
			polarRadius: 45,
		},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			tpl := cropAsTemplate(img, bm.tplX, bm.tplY, bm.tplW, bm.tplH)

			polar := Circle{X: bm.tplW / 2, Y: bm.tplH / 2, Radius: bm.polarRadius}
			tplCircleStats := GetImageCircleStats(tpl, polar)

			b.ReportAllocs()
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				_, _, _ = MatchCircleTemplate(img, imgIntArr, tpl, tplCircleStats, polar)
			}
		})
	}
}
