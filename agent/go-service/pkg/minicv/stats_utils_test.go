// Copyright (c) 2026 Harry Huang
package minicv

import (
	"image"
	"testing"
)

// generateBenchmarkImage creates a deterministic RGBA image for performance tests.
func generateBenchmarkImage(w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		off := y * img.Stride
		for x := range w {
			img.Pix[off] = uint8((x*31 + y*17) & 0xFF)
			img.Pix[off+1] = uint8((x*13 + y*29) & 0xFF)
			img.Pix[off+2] = uint8((x*7 + y*19) & 0xFF)
			img.Pix[off+3] = 255
			off += 4
		}
	}
	return img
}

func BenchmarkGetImageStats(b *testing.B) {
	benchmarks := []struct {
		name string
		w    int
		h    int
	}{
		{name: "720p", w: 1280, h: 720},
		{name: "1080p", w: 1920, h: 1080},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			img := generateBenchmarkImage(bm.w, bm.h)
			b.ReportAllocs()
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				_ = GetImageStats(img)
			}
		})
	}
}

func BenchmarkGetImageStatsInCircle(b *testing.B) {
	img := generateBenchmarkImage(1280, 720)

	benchmarks := []struct {
		name   string
		circle Circle
	}{
		{name: "720p_r23", circle: Circle{X: 640, Y: 360, Radius: 23}},
		{name: "720p_r45", circle: Circle{X: 640, Y: 360, Radius: 45}},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				_ = GetImageCircleStats(img, bm.circle)
			}
		})
	}
}

func BenchmarkGetIntegralArray(b *testing.B) {
	benchmarks := []struct {
		name string
		w    int
		h    int
	}{
		{name: "720p", w: 1280, h: 720},
		{name: "1080p", w: 1920, h: 1080},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			img := generateBenchmarkImage(bm.w, bm.h)
			b.ReportAllocs()
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				_ = GetIntegralArray(img)
			}
		})
	}
}
