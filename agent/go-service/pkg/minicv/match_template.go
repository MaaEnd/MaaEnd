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

// MatchTemplate performs template matching on the whole image,
// returns (x, y, score) of the best match
func MatchTemplate(
	img *image.RGBA,
	imgIntArr IntegralArray,
	tpl *image.RGBA,
	tplStats StatsResult,
) (int, int, float64) {
	iw, ih := img.Rect.Dx(), img.Rect.Dy()
	return MatchTemplateInArea(img, imgIntArr, tpl, tplStats, 0, 0, iw, ih)
}

// MatchTemplateInArea performs template matching such that the center of the template
// remains within the specified rectangle (ax, ay, aw, ah).
// Returns (x, y, score) of the best match, where (x, y) is the top-left corner.
func MatchTemplateInArea(
	img *image.RGBA,
	imgIntArr IntegralArray,
	tpl *image.RGBA,
	tplStats StatsResult,
	ax, ay, aw, ah int,
) (int, int, float64) {
	iw, ih := img.Rect.Dx(), img.Rect.Dy()
	tw, th := tpl.Rect.Dx(), tpl.Rect.Dy()

	// Calculate search bounds for the top-left corner (x, y)
	minX, minY := max(0, ax-tw/2), max(0, ay-th/2)
	maxX, maxY := min(iw-tw, ax+aw-tw/2), min(ih-th, ay+ah-th/2)

	if minX > maxX || minY > maxY {
		return 0, 0, 0.0
	}

	type result struct {
		x, y int
		s    float64
	}

	numWorkers, step := 4, 3
	resChan := make(chan result, numWorkers)

	for i := range numWorkers {
		go func(id int) {
			lx, ly, lm := 0, 0, -1.0
			for y := minY + id*step; y <= maxY; y += numWorkers * step {
				for x := minX; x <= maxX; x += step {
					s := ComputeNCC(img, imgIntArr, tpl, tplStats, x, y)
					if s > lm {
						lm, lx, ly = s, x, y
					}
				}
			}
			resChan <- result{lx, ly, lm}
		}(i)
	}

	bc := result{minX, minY, -1.0}
	for range numWorkers {
		r := <-resChan
		if r.s > bc.s {
			bc = r
		}
	}

	fm, fx, fy := bc.s, bc.x, bc.y
	// Fine-tuning pass around the best result
	for y := max(minY, bc.y-step+1); y <= min(maxY, bc.y+step-1); y++ {
		for x := max(minX, bc.x-step+1); x <= min(maxX, bc.x+step-1); x++ {
			s := ComputeNCC(img, imgIntArr, tpl, tplStats, x, y)
			if s > fm {
				fm, fx, fy = s, x, y
			}
		}
	}
	return fx, fy, fm
}
