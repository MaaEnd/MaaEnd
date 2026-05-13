package creditshopping

import maa "github.com/MaaXYZ/maa-framework-go/v4"

const component = "creditshopping"

// applyROIEdge 与 tools/i18n/sync_ocr_expected.py 中 roi_offset 语义一致。
func applyROIEdge(parent maa.Rect, off [4]int) maa.Rect {
	px, py, pw, ph := parent[0], parent[1], parent[2], parent[3]
	left, top, right, bottom := off[0], off[1], off[2], off[3]
	nw := pw + right - left
	nh := ph + bottom - top
	if nw <= 0 || nh <= 0 {
		return maa.Rect{}
	}
	return maa.Rect{px + left, py + top, nw, nh}
}

func rectValid(r maa.Rect) bool {
	return r[2] > 0 && r[3] > 0
}

func targetRect(r maa.Rect) maa.Target {
	return maa.NewTargetRect(r)
}
