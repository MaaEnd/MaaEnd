// Copyright (c) 2026 Harry Huang
package maptracker

import (
	"image"
	"image/draw"
	"math"
	"time"

	"github.com/MaaXYZ/maa-framework-go/v4"
	xdraw "golang.org/x/image/draw"
)

/* ******** Recognitions ******** */

// IntegralImage stores precomputed sums for O(1) area statistics
type IntegralImage struct {
	Sum   []float64
	SumSq []float64
	W, H  int
}

// NeedleStats holds the mean and standard deviation of the template
type NeedleStats struct {
	Mn float64 // Mean pixel value of the needle
	Dn float64 // Standard deviation of the needle
}

// NewIntegralImage computes the integral images for an RGBA image
func NewIntegralImage(img *image.RGBA) *IntegralImage {
	w, h := img.Rect.Dx(), img.Rect.Dy()
	sum := make([]float64, (w+1)*(h+1))
	sumSq := make([]float64, (w+1)*(h+1))
	stride := w + 1

	pix := img.Pix
	imgStride := img.Stride

	for y := 0; y < h; y++ {
		var rowSum, rowSumSq float64
		imgOffset := y * imgStride
		for x := 0; x < w; x++ {
			r, g, b := float64(pix[imgOffset]), float64(pix[imgOffset+1]), float64(pix[imgOffset+2])
			val := r + g + b
			valSq := r*r + g*g + b*b

			rowSum += val
			rowSumSq += valSq

			idx := (y+1)*stride + (x + 1)
			sum[idx] = sum[y*stride+(x+1)] + rowSum
			sumSq[idx] = sumSq[y*stride+(x+1)] + rowSumSq

			imgOffset += 4
		}
	}
	return &IntegralImage{Sum: sum, SumSq: sumSq, W: w, H: h}
}

// GetAreaStats returns (sum, sumSq) for a given rectangle in O(1)
func (ii *IntegralImage) GetAreaStats(x, y, w, h int) (float64, float64) {
	stride := ii.W + 1
	x1, y1, x2, y2 := x, y, x+w, y+h
	idx11, idx12 := y1*stride+x1, y1*stride+x2
	idx21, idx22 := y2*stride+x1, y2*stride+x2

	s := ii.Sum[idx22] - ii.Sum[idx12] - ii.Sum[idx21] + ii.Sum[idx11]
	sq := ii.SumSq[idx22] - ii.SumSq[idx12] - ii.SumSq[idx21] + ii.SumSq[idx11]
	return s, sq
}

func cropArea(img image.Image, centerX, centerY, radius int) image.Image {
	bounds := img.Bounds()
	y0, y1 := max(0, centerY-radius), min(bounds.Max.Y, centerY+radius+1)
	x0, x1 := max(0, centerX-radius), min(bounds.Max.X, centerX+radius+1)

	if sub, ok := img.(interface {
		SubImage(r image.Rectangle) image.Image
	}); ok {
		return sub.SubImage(image.Rect(x0, y0, x1, y1))
	}
	dst := image.NewRGBA(image.Rect(0, 0, x1-x0, y1-y0))
	draw.Draw(dst, dst.Bounds(), img, image.Point{x0, y0}, draw.Src)
	return dst
}

func scaleImage(img image.Image, scale float64) image.Image {
	if scale == 1.0 {
		return img
	}
	bounds := img.Bounds()
	newW, newH := int(float64(bounds.Dx())*scale), int(float64(bounds.Dy())*scale)
	dst := image.NewRGBA(image.Rect(0, 0, newW, newH))
	xdraw.BiLinear.Scale(dst, dst.Bounds(), img, bounds, xdraw.Over, nil)
	return dst
}

func rotateImageRGBA(imgRGBA *image.RGBA, angle float64) *image.RGBA {
	bounds := imgRGBA.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	cx, cy := float64(w)/2, float64(h)/2
	rad := angle * math.Pi / 180.0
	cos, sin := math.Cos(rad), math.Sin(rad)
	dst := image.NewRGBA(bounds)
	ds, ss := dst.Stride, imgRGBA.Stride
	dp, sp := dst.Pix, imgRGBA.Pix

	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			fx, fy := float64(x)-cx, float64(y)-cy
			sx, sy := int(fx*cos+fy*sin+cx), int(-fx*sin+fy*cos+cy)
			if sx >= 0 && sx < w && sy >= 0 && sy < h {
				copy(dp[y*ds+x*4:y*ds+x*4+4], sp[sy*ss+sx*4:sy*ss+sx*4+4])
			}
		}
	}
	return dst
}

func ToRGBA(img image.Image) *image.RGBA {
	if dst, ok := img.(*image.RGBA); ok {
		return dst
	}
	b := img.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(dst, dst.Bounds(), img, b.Min, draw.Src)
	return dst
}

func GetNeedleStats(nRGBA *image.RGBA) *NeedleStats {
	nW, nH := nRGBA.Rect.Dx(), nRGBA.Rect.Dy()
	var sn, ssn float64
	np, ns := nRGBA.Pix, nRGBA.Stride
	for y := 0; y < nH; y++ {
		o := y * ns
		for x := 0; x < nW; x++ {
			r, g, b := float64(np[o]), float64(np[o+1]), float64(np[o+2])
			sn += r + g + b
			ssn += r*r + g*g + b*b
			o += 4
		}
	}
	cnt := float64(nW * nH * 3)
	mn, dn := sn/cnt, math.Sqrt(ssn-cnt*(sn/cnt)*(sn/cnt))
	return &NeedleStats{Mn: mn, Dn: dn}
}

func MatchTemplateOptimized(
	hRGBA *image.RGBA,
	hInt *IntegralImage,
	nRGBA *image.RGBA,
	nStats *NeedleStats,
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
	numWorkers, step := 6, 3
	resChan := make(chan result, numWorkers)
	rows := maxY - minY + 1

	for i := 0; i < numWorkers; i++ {
		go func(id int) {
			lx, ly, lm := 0, 0, -1.0
			for y := minY + id*step; y < minY+rows; y += numWorkers * step {
				for x := minX; x <= maxX; x += step {
					s := computeNCCFast(hRGBA, hInt, nRGBA, x, y, nStats.Mn, nStats.Dn)
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
			s := computeNCCFast(hRGBA, hInt, nRGBA, x, y, nStats.Mn, nStats.Dn)
			if s > fm {
				fm, fx, fy = s, x, y
			}
		}
	}
	return fx, fy, fm
}

func computeNCCFast(hRGBA *image.RGBA, hInt *IntegralImage, nRGBA *image.RGBA, ox, oy int, mn, dn float64) float64 {
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
	sh, ssh := hInt.GetAreaStats(ox, oy, nW, nH)
	cnt := float64(nW * nH * 3)
	mh := sh / cnt
	dh := math.Sqrt(ssh - cnt*mh*mh)
	if dh < 1e-6 {
		return 0.0
	}
	return (shn - cnt*mh*mn) / (dh * dn)
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
	aw.ctx.RunActionDirect("Swipe", maa.NodeSwipeParam{
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
func (aw *ActionWrapper) RotateCamera(dx int, durationMillis int, delayMillis int) {
	cx, cy := WORK_W/2, WORK_H/2
	stepDelayMillis := delayMillis / 3
	aw.SwipeSync(cx, cy, dx, 0, durationMillis)
	aw.KeyDownSync(KEY_ALT, stepDelayMillis)
	aw.ClickSync(0, cx, cy, stepDelayMillis)
	aw.KeyUpSync(KEY_ALT, stepDelayMillis)
}

/* ******** Misc ******** */

// PrintUI displays a message on the UI
func PrintUI(ctx *maa.Context, content string) bool {
	overrideParam := map[string]any{
		"MapTrackerPrintUI": map[string]any{
			"pre_delay":  0,
			"post_delay": 0,
			"focus": map[string]any{
				"Node.Action.Starting": content,
			},
		},
	}
	ctx.RunTask("MapTrackerPrintUI", overrideParam)
	return true
}
