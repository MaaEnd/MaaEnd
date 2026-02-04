package itemtransfer

import maa "github.com/MaaXYZ/maa-framework-go/v4"

func Pointize(roi maa.Rect) maa.Rect {
	return maa.Rect{roi.X(), roi.Y(), 1, 1}
}
