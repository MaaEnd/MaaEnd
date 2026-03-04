// Copyright (c) 2026 Harry Huang
package maptracker

import (
	"image"
	"math"
	"time"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/minicv"
	"github.com/MaaXYZ/maa-framework-go/v4"
)

/* ******** Recognitions ******** */

func MatchTemplateOptimized(
	hRGBA *image.RGBA,
	hInt minicv.IntegralArray,
	nRGBA *image.RGBA,
	nStats minicv.StatsResult,
) (int, int, float64) {
	hW, hH, nW, nH := hRGBA.Rect.Dx(), hRGBA.Rect.Dy(), nRGBA.Rect.Dx(), nRGBA.Rect.Dy()
	if nW > hW || nH > hH {
		return 0, 0, 0.0
	}

	// Calculate search bounds for the top-left corner (x, y)
	minX, minY := 0, 0
	maxX, maxY := hW-nW, hH-nH

	type result struct {
		x, y int
		s    float64
	}

	numWorkers, step := 4, 3
	resChan := make(chan result, numWorkers)
	rows := maxY - minY + 1

	for i := 0; i < numWorkers; i++ {
		go func(id int) {
			lx, ly, lm := 0, 0, -1.0
			for y := minY + id*step; y < minY+rows; y += numWorkers * step {
				for x := minX; x <= maxX; x += step {
					s := computeNCCFast(hRGBA, hInt, nRGBA, x, y, nStats)
					if s > lm {
						lm, lx, ly = s, x, y
					}
				}
			}
			resChan <- result{lx, ly, lm}
		}(i)
	}

	bc := result{minX, minY, -1.0}
	for i := 0; i < numWorkers; i++ {
		r := <-resChan
		if r.s > bc.s {
			bc = r
		}
	}

	fm, fx, fy := bc.s, bc.x, bc.y
	// Fine-tuning pass around the best result
	for y := max(minY, bc.y-step+1); y < min(maxY+1, bc.y+step); y++ {
		for x := max(minX, bc.x-step+1); x < min(maxX+1, bc.x+step); x++ {
			s := computeNCCFast(hRGBA, hInt, nRGBA, x, y, nStats)
			if s > fm {
				fm, fx, fy = s, x, y
			}
		}
	}
	return fx, fy, fm
}

func MatchTemplateAround(
	hRGBA *image.RGBA,
	hInt minicv.IntegralArray,
	nRGBA *image.RGBA,
	nStats minicv.StatsResult,
	cx, cy, radius int,
) (int, int, float64) {
	hW, hH, nW, nH := hRGBA.Rect.Dx(), hRGBA.Rect.Dy(), nRGBA.Rect.Dx(), nRGBA.Rect.Dy()
	if nW > hW || nH > hH {
		return 0, 0, 0.0
	}

	// Calculate search bounds
	targetX := cx - nW/2
	targetY := cy - nH/2

	minX := max(0, targetX-radius)
	minY := max(0, targetY-radius)
	maxX := min(hW-nW, targetX+radius)
	maxY := min(hH-nH, targetY+radius)

	if minX > maxX || minY > maxY {
		return 0, 0, 0.0
	}

	type result struct {
		x, y int
		s    float64
	}

	numWorkers, step := 4, 3
	resChan := make(chan result, numWorkers)
	rows := maxY - minY + 1

	for i := 0; i < numWorkers; i++ {
		go func(id int) {
			lx, ly, lm := 0, 0, -1.0
			for y := minY + id*step; y < minY+rows; y += numWorkers * step {
				if y > maxY {
					break
				}
				for x := minX; x <= maxX; x += step {
					s := computeNCCFast(hRGBA, hInt, nRGBA, x, y, nStats)
					if s > lm {
						lm, lx, ly = s, x, y
					}
				}
			}
			resChan <- result{lx, ly, lm}
		}(i)
	}

	bc := result{minX, minY, -1.0}
	for i := 0; i < numWorkers; i++ {
		r := <-resChan
		if r.s > bc.s {
			bc = r
		}
	}

	fm, fx, fy := bc.s, bc.x, bc.y
	// Fine-tuning pass around the best result
	for y := max(minY, bc.y-step+1); y < min(maxY+1, bc.y+step); y++ {
		for x := max(minX, bc.x-step+1); x < min(maxX+1, bc.x+step); x++ {
			s := computeNCCFast(hRGBA, hInt, nRGBA, x, y, nStats)
			if s > fm {
				fm, fx, fy = s, x, y
			}
		}
	}
	return fx, fy, fm
}

func computeNCCFast(hRGBA *image.RGBA, hInt minicv.IntegralArray, nRGBA *image.RGBA, ox, oy int, nStats minicv.StatsResult) float64 {
	nW, nH := nRGBA.Rect.Dx(), nRGBA.Rect.Dy()
	hp, np, hs, ns := hRGBA.Pix, nRGBA.Pix, hRGBA.Stride, nRGBA.Stride
	var dot uint64
	rb := oy*hs + ox*4
	for y := 0; y < nH; y++ {
		hi, ni := rb, y*ns
		for x := 0; x < nW; x++ {
			dot += uint64(hp[hi]) * uint64(np[ni])
			dot += uint64(hp[hi+1]) * uint64(np[ni+1])
			dot += uint64(hp[hi+2]) * uint64(np[ni+2])
			hi += 4
			ni += 4
		}
		rb += hs
	}
	shn := float64(dot)
	sh, ssh := hInt.GetAreaIntegral(ox, oy, nW, nH)
	cnt := float64(nW * nH * 3)
	mh := sh / cnt
	vh := ssh - cnt*mh*mh
	if vh < 1e-3 {
		return 0.0
	}
	dh := math.Sqrt(vh)
	return (shn - cnt*mh*nStats.Mean) / (dh * nStats.Std)
}

/* ******** Actions ******** */

// ActionWrapper provides synchronized touch/key operations with built-in delays
type ActionWrapper struct {
	ctx  *maa.Context
	ctrl *maa.Controller
}

// NewActionWrapper creates a new ActionWrapper from a context
func NewActionWrapper(ctx *maa.Context, ctrl *maa.Controller) *ActionWrapper {
	return &ActionWrapper{ctx, ctrl}
}

// ClickSync performs a touch down and up at (x, y)
func (aw *ActionWrapper) ClickSync(contact, x, y int, delayMillis int) {
	aw.ctrl.PostTouchDown(int32(contact), int32(x), int32(y), 1).Wait()
	time.Sleep(time.Duration(delayMillis) * time.Millisecond)
	aw.ctrl.PostTouchUp(int32(contact)).Wait()
}

// SwipeSync performs a swipe from (x, y) to (x+dx, y+dy)
func (aw *ActionWrapper) SwipeSync(x, y, dx, dy int, delayMillis int) {
	aw.ctx.RunActionDirect("Swipe", maa.SwipeParam{
		Begin:     maa.NewTargetRect(maa.Rect{x, y, 4, 4}),
		End:       []maa.Target{maa.NewTargetRect(maa.Rect{x + dx, y + dy, 4, 4})},
		OnlyHover: true,
	}, maa.Rect{0, 0, 0, 0}, nil)
	time.Sleep(time.Duration(delayMillis) * time.Millisecond)
}

// KeyDownSync sends a key press
func (aw *ActionWrapper) KeyDownSync(keyCode int, delayMillis int) {
	aw.ctrl.PostKeyDown(int32(keyCode)).Wait()
	time.Sleep(time.Duration(delayMillis) * time.Millisecond)
}

// KeyUpSync sends a key release
func (aw *ActionWrapper) KeyUpSync(keyCode int, delayMillis int) {
	aw.ctrl.PostKeyUp(int32(keyCode)).Wait()
	time.Sleep(time.Duration(delayMillis) * time.Millisecond)
}

// KeyTypeSync sends a key press-release and waits
func (aw *ActionWrapper) KeyTypeSync(keyCode int, delayMillis int) {
	aw.ctrl.PostClickKey(int32(keyCode)).Wait()
	time.Sleep(time.Duration(delayMillis) * time.Millisecond)
}

// RotateCamera performs a camera rotation via series of mouse-keyboard operations
func (aw *ActionWrapper) RotateCamera(dx int, delayMillis int) {
	cx, cy := WORK_W/2, WORK_H/2
	stepDelayMillis := delayMillis / 4
	aw.SwipeSync(cx, cy, dx, 0, stepDelayMillis)
	aw.KeyDownSync(KEY_ALT, stepDelayMillis)
	aw.ClickSync(0, cx, cy, stepDelayMillis)
	aw.KeyUpSync(KEY_ALT, stepDelayMillis)
}
