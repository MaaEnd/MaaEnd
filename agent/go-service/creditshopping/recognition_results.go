package creditshopping

import (
	"sort"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

func recognitionResults(detail *maa.RecognitionDetail) []*maa.RecognitionResult {
	if detail == nil || detail.Results == nil {
		return nil
	}
	if len(detail.Results.Filtered) > 0 {
		return detail.Results.Filtered
	}
	if len(detail.Results.All) > 0 {
		return detail.Results.All
	}
	if detail.Results.Best != nil {
		return []*maa.RecognitionResult{detail.Results.Best}
	}
	return nil
}

func recognitionBoxes(detail *maa.RecognitionDetail) []maa.Rect {
	results := recognitionResults(detail)
	boxes := make([]maa.Rect, 0, len(results))
	for _, r := range results {
		box, ok := recognitionResultBox(r)
		if !ok || !rectValid(box) {
			continue
		}
		boxes = append(boxes, box)
	}
	return boxes
}

func recognitionResultBox(r *maa.RecognitionResult) (maa.Rect, bool) {
	if r == nil {
		return maa.Rect{}, false
	}
	if o, ok := r.AsOCR(); ok {
		return o.Box, true
	}
	if tm, ok := r.AsTemplateMatch(); ok {
		return tm.Box, true
	}
	if cm, ok := r.AsColorMatch(); ok {
		return cm.Box, true
	}
	return maa.Rect{}, false
}

func sortRectsVertical(boxes []maa.Rect) {
	sort.Slice(boxes, func(i, j int) bool {
		if boxes[i][1] != boxes[j][1] {
			return boxes[i][1] < boxes[j][1]
		}
		return boxes[i][0] < boxes[j][0]
	})
}
