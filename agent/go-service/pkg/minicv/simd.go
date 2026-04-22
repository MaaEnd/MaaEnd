//go:build !amd64 || purego

package minicv

import "unsafe"

func dotRGBA3Impl(imgPix, tplPix unsafe.Pointer, pixels int) uint64 {
	ipx := (*[1 << 30]byte)(imgPix)
	tpx := (*[1 << 30]byte)(tplPix)

	var dot uint64
	off := 0
	for range pixels {
		dot += uint64(ipx[off]) * uint64(tpx[off])
		dot += uint64(ipx[off+1]) * uint64(tpx[off+1])
		dot += uint64(ipx[off+2]) * uint64(tpx[off+2])
		off += 4
	}
	return dot
}
