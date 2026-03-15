// Copyright (c) 2026 Harry Huang
package minicv

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"math"

	xdraw "golang.org/x/image/draw"
)

// ImageCropRect crops a rectangular region from the image and clips it to bounds.
func ImageCropRect(img *image.RGBA, rect image.Rectangle) *image.RGBA {
	if img == nil {
		return image.NewRGBA(image.Rect(0, 0, 1, 1))
	}
	b := img.Bounds()
	clipped := rect.Intersect(b)
	if clipped.Empty() {
		return image.NewRGBA(image.Rect(0, 0, 1, 1))
	}
	dst := image.NewRGBA(image.Rect(0, 0, clipped.Dx(), clipped.Dy()))
	draw.Draw(dst, dst.Bounds(), img, clipped.Min, draw.Src)
	return dst
}

// ImageToBase64JPEG encodes image to JPEG and returns base64 without data URL prefix.
func ImageToBase64JPEG(img image.Image, quality int) (string, error) {
	if img == nil {
		return "", fmt.Errorf("nil image")
	}
	if quality < 1 {
		quality = 1
	}
	if quality > 100 {
		quality = 100
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: quality}); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(buf.Bytes()), nil
}

// ImageCropSquareByRadius crops a square region from the image centered at (centerX, centerY) with the given radius
func ImageCropSquareByRadius(img *image.RGBA, centerX, centerY, radius int) *image.RGBA {
	x1, x2 := max(img.Rect.Min.X, centerX-radius), min(img.Rect.Max.X, centerX+radius+1)
	y1, y2 := max(img.Rect.Min.Y, centerY-radius), min(img.Rect.Max.Y, centerY+radius+1)

	cropRect := image.Rect(x1, y1, x2, y2)
	dst := image.NewRGBA(image.Rect(0, 0, cropRect.Dx(), cropRect.Dy()))
	draw.Draw(dst, dst.Bounds(), img, cropRect.Min, draw.Src)
	return dst
}

// ImageRotate rotates an image by the given angle (degrees) around its center
func ImageRotate(img *image.RGBA, angle float64) *image.RGBA {
	w, h := img.Rect.Dx(), img.Rect.Dy()
	cx, cy := float64(w)/2, float64(h)/2

	rad := angle * math.Pi / 180.0
	cos, sin := math.Cos(rad), math.Sin(rad)

	dst := image.NewRGBA(img.Rect)
	dpx, ds := dst.Pix, dst.Stride
	ipx, is := img.Pix, img.Stride

	for y := range h {
		for x := range w {
			fx, fy := float64(x)-cx, float64(y)-cy
			sx, sy := int(fx*cos+fy*sin+cx), int(-fx*sin+fy*cos+cy)
			if sx >= 0 && sx < w && sy >= 0 && sy < h {
				copy(dpx[y*ds+x*4:y*ds+x*4+4], ipx[sy*is+sx*4:sy*is+sx*4+4])
			}
		}
	}
	return dst
}

// ImageScale scales an image by the given factor using bilinear interpolation
func ImageScale(img *image.RGBA, scale float64) *image.RGBA {
	if scale <= 0 {
		return img
	}
	if scale == 1.0 {
		return img
	}
	w, h := img.Rect.Dx(), img.Rect.Dy()
	newW, newH := int(float64(w)*scale), int(float64(h)*scale)
	if newW < 1 {
		newW = 1
	}
	if newH < 1 {
		newH = 1
	}
	dst := image.NewRGBA(image.Rect(0, 0, newW, newH))
	xdraw.BiLinear.Scale(dst, dst.Rect, img, img.Rect, xdraw.Over, nil)
	return dst
}

// ImageConvertRGBA converts any image.Image to *image.RGBA
func ImageConvertRGBA(img image.Image) *image.RGBA {
	if dst, ok := img.(*image.RGBA); ok {
		return dst
	}
	b := img.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(dst, dst.Bounds(), img, b.Min, draw.Src)
	return dst
}

// ImageDrawLine draws a line segment on an RGBA image with optional thickness.
func ImageDrawLine(img *image.RGBA, x1, y1, x2, y2 int, c color.RGBA, thickness int) {
	if img == nil {
		return
	}
	if thickness < 1 {
		thickness = 1
	}

	b := img.Bounds()
	x1, y1, x2, y2, ok := clipLineToRect(x1, y1, x2, y2, b)
	if !ok {
		return
	}

	dx := int(math.Abs(float64(x2 - x1)))
	dy := -int(math.Abs(float64(y2 - y1)))
	sx := -1
	if x1 < x2 {
		sx = 1
	}
	sy := -1
	if y1 < y2 {
		sy = 1
	}
	err := dx + dy

	for {
		ImageDrawFilledCircle(img, x1, y1, thickness/2, c)
		if x1 == x2 && y1 == y2 {
			break
		}
		e2 := 2 * err
		if e2 >= dy {
			err += dy
			x1 += sx
		}
		if e2 <= dx {
			err += dx
			y1 += sy
		}
	}
}

// ImageDrawFilledCircle draws a filled circle on an RGBA image.
func ImageDrawFilledCircle(img *image.RGBA, cx, cy, radius int, c color.RGBA) {
	if img == nil || radius < 0 {
		return
	}
	b := img.Bounds()
	if cx+radius < b.Min.X || cx-radius >= b.Max.X || cy+radius < b.Min.Y || cy-radius >= b.Max.Y {
		return
	}
	r2 := radius * radius
	for y := cy - radius; y <= cy+radius; y++ {
		if y < b.Min.Y || y >= b.Max.Y {
			continue
		}
		for x := cx - radius; x <= cx+radius; x++ {
			if x < b.Min.X || x >= b.Max.X {
				continue
			}
			dx := x - cx
			dy := y - cy
			if dx*dx+dy*dy <= r2 {
				img.SetRGBA(x, y, c)
			}
		}
	}
}

func clipLineToRect(x1, y1, x2, y2 int, rect image.Rectangle) (int, int, int, int, bool) {
	const (
		left = 1 << iota
		right
		bottom
		top
	)

	outCode := func(x, y int) int {
		code := 0
		if x < rect.Min.X {
			code |= left
		} else if x >= rect.Max.X {
			code |= right
		}
		if y < rect.Min.Y {
			code |= top
		} else if y >= rect.Max.Y {
			code |= bottom
		}
		return code
	}

	x1f, y1f := float64(x1), float64(y1)
	x2f, y2f := float64(x2), float64(y2)

	for {
		c1 := outCode(int(math.Round(x1f)), int(math.Round(y1f)))
		c2 := outCode(int(math.Round(x2f)), int(math.Round(y2f)))
		if (c1 | c2) == 0 {
			return int(math.Round(x1f)), int(math.Round(y1f)), int(math.Round(x2f)), int(math.Round(y2f)), true
		}
		if (c1 & c2) != 0 {
			return 0, 0, 0, 0, false
		}

		out := c1
		if out == 0 {
			out = c2
		}

		dx := x2f - x1f
		dy := y2f - y1f
		if dx == 0 && dy == 0 {
			return 0, 0, 0, 0, false
		}

		var x, y float64
		if (out & top) != 0 {
			y = float64(rect.Min.Y)
			x = x1f + dx*(y-y1f)/dy
		} else if (out & bottom) != 0 {
			y = float64(rect.Max.Y - 1)
			x = x1f + dx*(y-y1f)/dy
		} else if (out & right) != 0 {
			x = float64(rect.Max.X - 1)
			y = y1f + dy*(x-x1f)/dx
		} else {
			x = float64(rect.Min.X)
			y = y1f + dy*(x-x1f)/dx
		}

		if out == c1 {
			x1f, y1f = x, y
		} else {
			x2f, y2f = x, y
		}
	}
}
