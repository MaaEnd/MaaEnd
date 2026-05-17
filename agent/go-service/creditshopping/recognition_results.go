package creditshopping

import (
	"sort"
	"strings"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

type ocrNameHit struct {
	Box  maa.Rect
	Text string
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

func findRecognitionDetailByName(detail *maa.RecognitionDetail, targetName string) *maa.RecognitionDetail {
	if detail == nil {
		return nil
	}
	if detail.Name == targetName {
		return detail
	}
	for _, child := range detail.CombinedResult {
		if found := findRecognitionDetailByName(child, targetName); found != nil {
			return found
		}
	}
	return nil
}

// ocrNameHitsFromRecordItemName 从 RecordItemName（And）的 CombinedResult 中提取
// ItemNameOCR 子节点的全部 filtered 命中（名称 + 字框）。
func ocrNameHitsFromRecordItemName(detail *maa.RecognitionDetail) []ocrNameHit {
	ocrDetail := findRecognitionDetailByName(detail, pipelineNodeItemNameOCR)
	if ocrDetail == nil {
		return nil
	}
	results := recognitionResults(ocrDetail)
	if len(results) == 0 {
		return nil
	}
	out := make([]ocrNameHit, 0, len(results))
	for _, r := range results {
		o, ok := r.AsOCR()
		if !ok {
			continue
		}
		text := strings.TrimSpace(o.Text)
		if text == "" || !rectValid(o.Box) {
			continue
		}
		out = append(out, ocrNameHit{Box: o.Box, Text: text})
	}
	sortOCRNameHits(out)
	return out
}

func sortOCRNameHits(hits []ocrNameHit) {
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].Box[1] != hits[j].Box[1] {
			return hits[i].Box[1] < hits[j].Box[1]
		}
		return hits[i].Box[0] < hits[j].Box[0]
	})
}
