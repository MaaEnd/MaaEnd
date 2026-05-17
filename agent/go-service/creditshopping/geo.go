package creditshopping

import maa "github.com/MaaXYZ/maa-framework-go/v4"

const component = "creditshopping"

func rectValid(r maa.Rect) bool {
	return r[2] > 0 && r[3] > 0
}

func targetRect(r maa.Rect) maa.Target {
	return maa.NewTargetRect(r)
}
