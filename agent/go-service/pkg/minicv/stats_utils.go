// Copyright (c) 2026 Harry Huang
package minicv

import (
	"image"
	"math"
)

// StatsResult holds the mean and unnormalized standard deviation of pixel values in an image
type StatsResult struct {
	Mean float64 // Mean value
	Std  float64 // Standard deviation value (unnormalized)
}

// IntegralArray stores precomputed sums for O(1) area statistics
type IntegralArray struct {
	Sum   []float64
	SumSq []float64
	W, H  int
}

// GetImageStats computes the mean and standard deviation of pixel values in an image
func GetImageStats(img *image.RGBA) StatsResult {
	w, h := img.Rect.Dx(), img.Rect.Dy()
	ipx, is := img.Pix, img.Stride

	sum := 0.0
	sumSq := 0.0

	for y := range h {
		off := y * is
		for range w {
			r, g, b := float64(ipx[off]), float64(ipx[off+1]), float64(ipx[off+2])
			sum += r + g + b
			sumSq += r*r + g*g + b*b
			off += 4
		}
	}

	count := float64(w * h * 3)
	mean := sum / count
	variance := sumSq - count*(mean*mean)
	if variance < 0 {
		return StatsResult{Mean: mean, Std: 0}
	}
	return StatsResult{mean, math.Sqrt(variance)}
}

// GetIntegralArray computes the integral array for an image
func GetIntegralArray(img *image.RGBA) IntegralArray {
	w, h := img.Rect.Dx(), img.Rect.Dy()

	sumArr := make([]float64, (w+1)*(h+1))
	sumSqArr := make([]float64, (w+1)*(h+1))
	stride := w + 1

	ipx, is := img.Pix, img.Stride

	for y := range h {
		var sumRow, sumSqRow float64
		off := y * is
		for x := range w {
			r, g, b := float64(ipx[off]), float64(ipx[off+1]), float64(ipx[off+2])
			sumRow += r + g + b
			sumSqRow += r*r + g*g + b*b

			idx := (y+1)*stride + (x + 1)
			sumArr[idx] = sumArr[y*stride+(x+1)] + sumRow
			sumSqArr[idx] = sumSqArr[y*stride+(x+1)] + sumSqRow
			off += 4
		}
	}
	return IntegralArray{Sum: sumArr, SumSq: sumSqArr, W: w, H: h}
}

// GetAreaIntegral returns (sum, sumSq) for a given rectangle area using the integral array
func (ia *IntegralArray) GetAreaIntegral(x, y, w, h int) (float64, float64) {
	stride := ia.W + 1
	x1, y1, x2, y2 := x, y, x+w, y+h
	idx11, idx12 := y1*stride+x1, y1*stride+x2
	idx21, idx22 := y2*stride+x1, y2*stride+x2

	sum := ia.Sum[idx22] - ia.Sum[idx12] - ia.Sum[idx21] + ia.Sum[idx11]
	sumSq := ia.SumSq[idx22] - ia.SumSq[idx12] - ia.SumSq[idx21] + ia.SumSq[idx11]
	return sum, sumSq
}
