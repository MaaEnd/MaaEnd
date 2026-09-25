package autostockpile

import (
	"regexp"
	"strings"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

var priceRe = regexp.MustCompile(`^(\d{3,4})$`)

type ocrTextPolicy int

const (
	ocrTextPolicyFilteredOnly ocrTextPolicy = iota
	ocrTextPolicyBestOnly
)

func filteredRecognitionResults(detail *maa.RecognitionDetail) []*maa.RecognitionResult {
	if detail == nil || detail.Results == nil {
		return nil
	}
	return detail.Results.Filtered
}

// ocrCandidates 按 policy 提取当前识别详情中的 OCR 结果，是唯一的提取入口。
func ocrCandidates(detail *maa.RecognitionDetail, policy ocrTextPolicy) []*maa.OCRResult {
	var results []*maa.RecognitionResult
	switch policy {
	case ocrTextPolicyFilteredOnly:
		results = filteredRecognitionResults(detail)
	case ocrTextPolicyBestOnly:
		if detail != nil && detail.Results != nil && detail.Results.Best != nil {
			results = []*maa.RecognitionResult{detail.Results.Best}
		}
	}

	candidates := make([]*maa.OCRResult, 0, len(results))
	for _, result := range results {
		if result == nil {
			continue
		}
		ocrResult, ok := result.AsOCR()
		if !ok {
			continue
		}
		candidates = append(candidates, ocrResult)
	}
	return candidates
}

func filteredOCRCandidates(detail *maa.RecognitionDetail) []*maa.OCRResult {
	return ocrCandidates(detail, ocrTextPolicyFilteredOnly)
}

func ocrTextCandidates(detail *maa.RecognitionDetail, policy ocrTextPolicy) []string {
	texts := make([]string, 0)
	seen := make(map[string]struct{})
	for _, ocrResult := range ocrCandidates(detail, policy) {
		text := strings.TrimSpace(ocrResult.Text)
		if text == "" {
			continue
		}
		if _, exists := seen[text]; exists {
			continue
		}
		seen[text] = struct{}{}
		texts = append(texts, text)
	}

	return texts
}

func bestTemplateHit(detail *maa.RecognitionDetail) (maa.Rect, bool) {
	if detail == nil || detail.Results == nil || detail.Results.Best == nil {
		return maa.Rect{}, false
	}

	tm, ok := detail.Results.Best.AsTemplateMatch()
	if !ok {
		return maa.Rect{}, false
	}

	return tm.Box, true
}
