package creditshopping

import (
	"strings"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

type ocrNameHit struct {
	Box  maa.Rect
	Text string
	ID   string
}

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

func bestOCRText(detail *maa.RecognitionDetail) string {
	if detail == nil || detail.Results == nil {
		return ""
	}
	if detail.Results.Best != nil {
		if o, ok := detail.Results.Best.AsOCR(); ok {
			return strings.TrimSpace(o.Text)
		}
	}
	for _, r := range detail.Results.Filtered {
		if r == nil {
			continue
		}
		if o, ok := r.AsOCR(); ok {
			return strings.TrimSpace(o.Text)
		}
	}
	return ""
}
