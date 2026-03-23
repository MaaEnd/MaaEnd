package minicv

import (
	"fmt"
	"image"
	_ "image/png"
	"os"
	"sync"
)

// Template represents a preloaded template image along with its integral array and statistics for matching.
type Template struct {
	Image    *image.RGBA
	Integral IntegralArray
	Stats    StatsResult
}

// TemplateLoader provides lazy-loading of template objects.
type TemplateLoader struct {
	filePathProvider func() string
	templateOnce     sync.Once
	template         *Template
	templateErr      error
}

// NewTemplateLoaderOfPath creates a new TemplateLoader using the given image file path.
func NewTemplateLoaderOfPath(filePath string) *TemplateLoader {
	return &TemplateLoader{filePathProvider: func() string { return filePath }}
}

// NewTemplateLoaderOfDynamicPath creates a new TemplateLoader using a dynamic file path provider function.
// Note that the file path provider function will be called only once during the first Get() call,
// and the result will be cached permanently for subsequent calls.
func NewTemplateLoaderOfDynamicPath(filePathProvider func() string) *TemplateLoader {
	return &TemplateLoader{filePathProvider: filePathProvider}
}

// Get returns the loaded template or an error if loading failed.
func (i *TemplateLoader) Get() (*Template, error) {
	i.templateOnce.Do(func() {
		// Check file path validity
		filePath := i.filePathProvider()
		if filePath == "" {
			i.templateErr = fmt.Errorf("given image file path is empty")
			return
		}
		if _, err := os.Stat(filePath); err != nil {
			i.templateErr = fmt.Errorf("given image file path is unavailable: %w", err)
			return
		}

		// Open image file
		f, err := os.Open(filePath)
		if err != nil {
			i.templateErr = err
			return
		}
		defer f.Close()

		// Read image to memory
		img, _, err := image.Decode(f)
		if err != nil {
			i.templateErr = err
			return
		}

		// Compute results
		imgRGBA := ImageConvertRGBA(img)
		integral := GetIntegralArray(imgRGBA)
		stats := GetImageStats(imgRGBA)

		// Validate sanity
		if stats.Std < 1e-6 {
			i.templateErr = fmt.Errorf("template image cannot have near-zero standard deviation")
			return
		}

		i.template = &Template{imgRGBA, integral, stats}
	})

	// Return cached results
	return i.template, i.templateErr
}

func subpixelOffset(neg, pos float64) float64 {
	wn := max(0.0, neg)
	wp := max(0.0, pos)
	wn2 := wn * wn
	wp2 := wp * wp

	sum := wn2 + wp2
	if sum < 1e-12 {
		return 0.0
	}

	offset := (wp2 - wn2) / sum
	return min(1.0, max(-1.0, offset))
}

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
// returns (x, y, val) of the best match, where x and y are subpixel-accurate coordinates.
func MatchTemplate(
	img *image.RGBA,
	imgIntArr IntegralArray,
	tpl *image.RGBA,
	tplStats StatsResult,
) (x, y, val float64) {
	iw, ih := img.Rect.Dx(), img.Rect.Dy()
	return MatchTemplateInArea(img, imgIntArr, tpl, tplStats, [4]int{0, 0, iw, ih})
}

// MatchTemplateInArea performs template matching such that the center of the template
// remains within the specified area's rectangle (x, y, w, h).
// Returns (x, y, val) of the best match, where (x, y) is the top-left corner with subpixel accuracy.
func MatchTemplateInArea(
	img *image.RGBA,
	imgIntArr IntegralArray,
	tpl *image.RGBA,
	tplStats StatsResult,
	rect [4]int,
) (x, y, val float64) {
	ax, ay, aw, ah := rect[0], rect[1], rect[2], rect[3]
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

	numWorkers, stepLen := 4, 3
	resChan := make(chan result, numWorkers)

	for i := range numWorkers {
		go func(id int) {
			lx, ly, lm := 0, 0, -1.0
			for y := minY + id*stepLen; y <= maxY; y += numWorkers * stepLen {
				for x := minX; x <= maxX; x += stepLen {
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
	for y := max(minY, bc.y-stepLen+1); y <= min(maxY, bc.y+stepLen-1); y++ {
		for x := max(minX, bc.x-stepLen+1); x <= min(maxX, bc.x+stepLen-1); x++ {
			s := ComputeNCC(img, imgIntArr, tpl, tplStats, x, y)
			if s > fm {
				fm, fx, fy = s, x, y
			}
		}
	}

	upNCC, downNCC := fm, fm
	leftNCC, rightNCC := fm, fm

	if fy-1 >= minY {
		upNCC = ComputeNCC(img, imgIntArr, tpl, tplStats, fx, fy-1)
	}
	if fy+1 <= maxY {
		downNCC = ComputeNCC(img, imgIntArr, tpl, tplStats, fx, fy+1)
	}
	if fx-1 >= minX {
		leftNCC = ComputeNCC(img, imgIntArr, tpl, tplStats, fx-1, fy)
	}
	if fx+1 <= maxX {
		rightNCC = ComputeNCC(img, imgIntArr, tpl, tplStats, fx+1, fy)
	}

	subX := float64(fx) + subpixelOffset(leftNCC, rightNCC)
	subY := float64(fy) + subpixelOffset(upNCC, downNCC)

	return subX, subY, fm
}

// MatchTemplateAnyScale performs iterative template matching over a scale range.
// The number of iterations is defined by len(steps), and each element controls the
// sampling count for that iteration.
// Returns (x, y, val, bestScale) for the best match found across all iterations.
func MatchTemplateAnyScale(
	img *image.RGBA,
	imgIntArr IntegralArray,
	tpl *image.RGBA,
	minScale, maxScale float64,
	steps []int,
) (x, y, val, bestScale float64) {
	if minScale > maxScale {
		minScale, maxScale = maxScale, minScale
	}
	if maxScale <= 0 {
		return 0, 0, 0, 0
	}
	if minScale <= 0 {
		minScale = 1e-6
	}
	minScale0, maxScale0 := minScale, maxScale
	if len(steps) == 0 {
		steps = []int{1}
	}

	bestX, bestY, bestScore, bestScale := 0.0, 0.0, -1.0, minScale

	for _, stepCount := range steps {
		if minScale > maxScale {
			break
		}
		if stepCount < 1 {
			stepCount = 1
		}

		stepLen := 0.0
		if stepCount > 1 {
			stepLen = (maxScale - minScale) / float64(stepCount-1)
		}

		iterBestIdx := 0
		iterBestScale := minScale
		iterBestX, iterBestY, iterBestScore := 0.0, 0.0, -1.0

		type result struct {
			idx   int
			scale float64
			x     float64
			y     float64
			score float64
			valid bool
		}

		workerCount := min(stepCount, 8)
		resChan := make(chan result, stepCount)

		for workerID := range workerCount {
			go func(id int) {
				for idx := id; idx < stepCount; idx += workerCount {
					scale := minScale
					if stepCount == 1 {
						scale = (minScale + maxScale) * 0.5
					} else {
						scale = minScale + float64(idx)*stepLen
					}

					if scale <= 0 {
						resChan <- result{idx: idx, scale: scale, score: -1.0, valid: false}
						continue
					}

					scaledTpl := ImageScale(tpl, scale)
					scaledStats := GetImageStats(scaledTpl)
					if scaledStats.Std < 1e-12 {
						resChan <- result{
							idx:   idx,
							scale: scale,
							score: -1.0,
							valid: false,
						}
						continue
					}

					x, y, score := MatchTemplate(img, imgIntArr, scaledTpl, scaledStats)

					resChan <- result{
						idx:   idx,
						scale: scale,
						x:     x,
						y:     y,
						score: score,
						valid: true,
					}
				}
			}(workerID)
		}

		for range stepCount {
			res := <-resChan
			if res.valid {
				if res.score > iterBestScore {
					iterBestScore = res.score
					iterBestX = res.x
					iterBestY = res.y
					iterBestScale = res.scale
					iterBestIdx = res.idx
				}
			}
		}

		if iterBestScore > bestScore {
			bestScore = iterBestScore
			bestX = iterBestX
			bestY = iterBestY
			bestScale = iterBestScale
		}

		if stepLen <= 0 {
			break
		}

		switch iterBestIdx {
		case 0:
			minScale = iterBestScale
			maxScale = iterBestScale + stepLen
		case stepCount - 1:
			minScale = iterBestScale - stepLen
			maxScale = iterBestScale
		default:
			minScale = iterBestScale - stepLen
			maxScale = iterBestScale + stepLen
		}

		minScale = max(minScale0, minScale)
		maxScale = min(maxScale0, maxScale)
		if minScale > maxScale {
			clamped := min(max(iterBestScale, minScale0), maxScale0)
			minScale = clamped
			maxScale = clamped
		}
	}

	if bestScore < 0 {
		return 0, 0, 0, 0
	}

	return bestX, bestY, bestScore, bestScale
}
