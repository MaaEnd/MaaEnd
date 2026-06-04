package minicv

import (
	"image"
	"math"
	"unsafe"
)

func dotRGBA3(imgPix, tplPix *uint8, pixels int) uint64 {
	if pixels <= 0 {
		return 0
	}
	return dotRGBA3Impl(unsafe.Pointer(imgPix), unsafe.Pointer(tplPix), pixels)
}

// ComputeNCC computes the normalized cross-correlation between a rectangle region in the haystack image
// and a template image, using precomputed integral array for efficiency.
func ComputeNCC(img *image.RGBA, imgIntArr IntegralArray, tpl *image.RGBA, tplStats StatsResult, ox, oy int) float64 {
	iw, ih := img.Rect.Dx(), img.Rect.Dy()
	tw, th := tpl.Rect.Dx(), tpl.Rect.Dy()
	if ox < 0 || oy < 0 || ox+tw > iw || oy+th > ih {
		return 0.0
	}

	ipx, is := img.Pix, img.Stride
	tpx, ts := tpl.Pix, tpl.Stride

	var dot uint64
	iOff := oy*is + ox*4
	tOff := 0
	for range th {
		dot += dotRGBA3(&ipx[iOff], &tpx[tOff], tw)
		iOff += is
		tOff += ts
	}

	count := float64(tw * th * 3)
	imgStats := imgIntArr.GetAreaStats(ox, oy, tw, th)
	stdProd := imgStats.Std * tplStats.Std
	if stdProd < 1e-12 {
		return 0.0
	}
	return (float64(dot) - count*imgStats.Mean*tplStats.Mean) / stdProd
}

type maskSpan struct {
	Y  int
	X0 int
	X1 int
}

func templateRGBMaskValue(px []uint8, off int) uint32 {
	return uint32(px[off])<<16 | uint32(px[off+1])<<8 | uint32(px[off+2])
}

func buildMaskSpans(tpl *image.RGBA, maskColorRGB888 int32) ([]maskSpan, int, StatsResult) {
	w, h := tpl.Rect.Dx(), tpl.Rect.Dy()
	maskRGB := uint32(maskColorRGB888) & 0xFFFFFF

	tplPix, tplStride := tpl.Pix, tpl.Stride
	spans := make([]maskSpan, 0, h)
	pixelCount := 0
	var sum uint64
	var sumSq uint64

	for y := range h {
		spanStart := -1
		off := y * tplStride
		for x := range w {
			if templateRGBMaskValue(tplPix, off) == maskRGB {
				if spanStart >= 0 {
					spans = append(spans, maskSpan{Y: y, X0: spanStart, X1: x - 1})
					spanStart = -1
				}
				off += 4
				continue
			}

			if spanStart < 0 {
				spanStart = x
			}
			r, g, b := uint64(tplPix[off]), uint64(tplPix[off+1]), uint64(tplPix[off+2])
			sum += r + g + b
			sumSq += r*r + g*g + b*b
			pixelCount++
			off += 4
		}
		if spanStart >= 0 {
			spans = append(spans, maskSpan{Y: y, X0: spanStart, X1: w - 1})
		}
	}

	if pixelCount == 0 {
		return spans, 0, StatsResult{}
	}

	count := float64(pixelCount * 3)
	mean := float64(sum) / count
	variance := float64(sumSq) - count*(mean*mean)
	if variance < 1e-12 {
		return spans, pixelCount, StatsResult{Mean: mean, Std: 0}
	}
	return spans, pixelCount, StatsResult{Mean: mean, Std: math.Sqrt(variance)}
}

// ComputeNCCWithMaskSpans computes normalized cross-correlation using template mask spans.
func ComputeNCCWithMaskSpans(
	img *image.RGBA,
	imgIntArr IntegralArray,
	tpl *image.RGBA,
	tplMaskStats StatsResult,
	spans []maskSpan,
	pixelCount int,
	ox, oy int,
) float64 {
	if pixelCount <= 0 || tplMaskStats.Std < 1e-12 {
		return 0.0
	}

	iw, ih := img.Rect.Dx(), img.Rect.Dy()
	tw, th := tpl.Rect.Dx(), tpl.Rect.Dy()
	if ox < 0 || oy < 0 || ox+tw > iw || oy+th > ih {
		return 0.0
	}

	imgPix, imgStride := img.Pix, img.Stride
	tplPix, tplStride := tpl.Pix, tpl.Stride

	var dot uint64
	var sumI float64
	var sumI2 float64

	for _, sp := range spans {
		width := sp.X1 - sp.X0 + 1

		rowSum, rowSumSq := imgIntArr.GetRowRangeIntegral(oy+sp.Y, ox+sp.X0, width)
		sumI += rowSum
		sumI2 += rowSumSq

		imgOff := (oy+sp.Y)*imgStride + (ox+sp.X0)*4
		tplOff := sp.Y*tplStride + sp.X0*4
		dot += dotRGBA3(&imgPix[imgOff], &tplPix[tplOff], width)
	}

	count := float64(pixelCount * 3)
	imgMean := sumI / count
	imgVar := sumI2 - count*imgMean*imgMean
	if imgVar < 1e-12 {
		return 0.0
	}
	imgStd := math.Sqrt(imgVar)
	stdProd := imgStd * tplMaskStats.Std
	if stdProd < 1e-12 {
		return 0.0
	}

	return (float64(dot) - count*imgMean*tplMaskStats.Mean) / stdProd
}

// ComputeNCCMatrix computes normalized cross-correlation for every valid template top-left position.
func ComputeNCCMatrix(img *image.RGBA, imgIntArr IntegralArray, tpl *image.RGBA, tplStats StatsResult) [][]float64 {
	if img == nil || tpl == nil || tplStats.Std < 1e-12 {
		return nil
	}

	iw, ih := img.Rect.Dx(), img.Rect.Dy()
	tw, th := tpl.Rect.Dx(), tpl.Rect.Dy()
	mw, mh := iw-tw+1, ih-th+1
	if tw <= 0 || th <= 0 || mw <= 0 || mh <= 0 {
		return nil
	}

	matrix := make([][]float64, mh)
	for y := range mh {
		matrix[y] = make([]float64, mw)
	}

	workerCount := min(8, mh)
	runMatchWorkers(workerCount, func(id int) {
		for y := id; y < mh; y += workerCount {
			row := matrix[y]
			for x := range mw {
				row[x] = ComputeNCC(img, imgIntArr, tpl, tplStats, x, y)
			}
		}
	})

	return matrix
}

// ComputeNCCMatrixWithMask computes normalized cross-correlation for every valid position while ignoring mask-color template pixels.
func ComputeNCCMatrixWithMask(
	img *image.RGBA,
	imgIntArr IntegralArray,
	tpl *image.RGBA,
	tplStats StatsResult,
	maskColorRGB888 int32,
) [][]float64 {
	if img == nil || tpl == nil {
		return nil
	}

	iw, ih := img.Rect.Dx(), img.Rect.Dy()
	tw, th := tpl.Rect.Dx(), tpl.Rect.Dy()
	mw, mh := iw-tw+1, ih-th+1
	if tw <= 0 || th <= 0 || mw <= 0 || mh <= 0 {
		return nil
	}

	spans, pixelCount, tplMaskStats := buildMaskSpans(tpl, maskColorRGB888)
	if pixelCount == 0 {
		return nil
	}
	if pixelCount == tw*th {
		tplMaskStats = tplStats
	}
	if tplMaskStats.Std < 1e-12 {
		return nil
	}

	matrix := make([][]float64, mh)
	for y := range mh {
		matrix[y] = make([]float64, mw)
	}

	workerCount := min(8, mh)
	runMatchWorkers(workerCount, func(id int) {
		for y := id; y < mh; y += workerCount {
			row := matrix[y]
			for x := range mw {
				row[x] = ComputeNCCWithMaskSpans(img, imgIntArr, tpl, tplMaskStats, spans, pixelCount, x, y)
			}
		}
	})

	return matrix
}
