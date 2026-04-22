//go:build amd64 && !purego

package minicv

import "unsafe"

//go:noescape
func dotRGBA3SIMD(imgPix, tplPix unsafe.Pointer, pixels int) uint64

func dotRGBA3Impl(imgPix, tplPix unsafe.Pointer, pixels int) uint64 {
	return dotRGBA3SIMD(imgPix, tplPix, pixels)
}
