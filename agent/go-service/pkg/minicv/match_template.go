package minicv

import (
	"image"
)

// ComputeNCC computes the normalized cross-correlation between a rectangle region in the haystack image
// and a template image, using precomputed integral array for efficiency
func ComputeNCC(img *image.RGBA, imgIntArr IntegralArray, tpl *image.RGBA, tplStats StatsResult, ox, oy int) float64 {
	iw, ih := img.Rect.Dx(), img.Rect.Dy()
	tw, th := tpl.Rect.Dx(), tpl.Rect.Dy()
	if ox < 0 || oy < 0 || ox+tw > iw || oy+th > ih {
		return 0.0
	}

	ipx, is := img.Pix, img.Stride
	tpx, ts := tpl.Pix, tpl.Stride

	var dot uint64
	iOffBase := oy*is + ox*4
	for y := range th {
		iOff := iOffBase
		tOff := y * ts
		for range tw {
			dot += uint64(ipx[iOff]) * uint64(tpx[tOff])
			dot += uint64(ipx[iOff+1]) * uint64(tpx[tOff+1])
			dot += uint64(ipx[iOff+2]) * uint64(tpx[tOff+2])
			iOff += 4
			tOff += 4
		}
		iOffBase += is
	}

	count := float64(tw * th * 3)
	imgStats := imgIntArr.GetAreaStats(ox, oy, tw, th)
	stdProd := imgStats.Std * tplStats.Std
	if stdProd < 1e-12 {
		return 0.0
	}
	return (float64(dot) - count*imgStats.Mean*tplStats.Mean) / stdProd
}
