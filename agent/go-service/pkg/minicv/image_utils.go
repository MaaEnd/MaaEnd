// Copyright (c) 2026 Harry Huang
package minicv

import (
	"image"
	"image/draw"
	"math"

	xdraw "golang.org/x/image/draw"
)

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
