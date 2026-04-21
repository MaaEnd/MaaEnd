// Copyright (c) 2026 Harry Huang
package minicv

import (
	"image"
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
