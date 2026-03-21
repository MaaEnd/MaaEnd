package quantizedsliding

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

func extractHitBox(recognitionDetail *maa.RecognitionDetail) ([]int, bool) {
	if recognitionDetail == nil {
		return nil, false
	}

	if len(recognitionDetail.Box) >= 4 {
		return []int{recognitionDetail.Box[0], recognitionDetail.Box[1], recognitionDetail.Box[2], recognitionDetail.Box[3]}, true
	}

	if recognitionDetail.Results == nil {
		return nil, false
	}

	if recognitionDetail.Results.Best != nil {
		if tm, ok := recognitionDetail.Results.Best.AsTemplateMatch(); ok {
			return []int{tm.Box.X(), tm.Box.Y(), tm.Box.Width(), tm.Box.Height()}, true
		}
		if ocr, ok := recognitionDetail.Results.Best.AsOCR(); ok {
			return []int{ocr.Box.X(), ocr.Box.Y(), ocr.Box.Width(), ocr.Box.Height()}, true
		}
	}

	for _, result := range recognitionDetail.Results.Filtered {
		if tm, ok := result.AsTemplateMatch(); ok {
			return []int{tm.Box.X(), tm.Box.Y(), tm.Box.Width(), tm.Box.Height()}, true
		}
		if ocr, ok := result.AsOCR(); ok {
			return []int{ocr.Box.X(), ocr.Box.Y(), ocr.Box.Width(), ocr.Box.Height()}, true
		}
	}

	for _, result := range recognitionDetail.Results.All {
		if tm, ok := result.AsTemplateMatch(); ok {
			return []int{tm.Box.X(), tm.Box.Y(), tm.Box.Width(), tm.Box.Height()}, true
		}
		if ocr, ok := result.AsOCR(); ok {
			return []int{ocr.Box.X(), ocr.Box.Y(), ocr.Box.Width(), ocr.Box.Height()}, true
		}
	}

	return nil, false
}

func parseOCRText(recognitionDetail *maa.RecognitionDetail) (int, error) {
	if recognitionDetail == nil {
		return 0, fmt.Errorf("recognition detail is nil")
	}

	text := extractOCRText(recognitionDetail)

	if text == "" {
		return 0, fmt.Errorf("ocr text not found in recognition detail")
	}

	var digits strings.Builder
	for _, r := range text {
		if r >= '0' && r <= '9' {
			digits.WriteRune(r)
		}
	}
	if digits.Len() == 0 {
		return 0, fmt.Errorf("ocr text has no digit: %s", text)
	}

	value, err := strconv.Atoi(digits.String())
	if err != nil {
		return 0, err
	}

	return value, nil
}

func extractOCRText(detail *maa.RecognitionDetail) string {
	if detail == nil {
		return ""
	}

	if text := extractOCRTextFromResults(detail.Results); text != "" {
		return text
	}

	for _, child := range detail.CombinedResult {
		if text := extractOCRText(child); text != "" {
			return text
		}
	}

	return extractOCRTextFromDetailJSON(detail.DetailJson)
}

func extractOCRTextFromResults(results *maa.RecognitionResults) string {
	if results == nil {
		return ""
	}

	candidates := []ocrCandidate{
		buildOCRCandidate([]*maa.RecognitionResult{results.Best}, 0),
		buildOCRCandidate(results.Filtered, 1),
		buildOCRCandidate(results.All, 2),
	}

	return selectOCRCandidate(candidates).text
}

func extractOCRTextFromDetailJSON(detailJSON string) string {
	detailJSON = strings.TrimSpace(detailJSON)
	if detailJSON == "" || detailJSON == "null" {
		return ""
	}

	var direct struct {
		Best struct {
			Detail json.RawMessage `json:"detail"`
			Text   string          `json:"text"`
		} `json:"best"`
		Detail json.RawMessage `json:"detail"`
		Text   string          `json:"text"`
	}
	if err := json.Unmarshal([]byte(detailJSON), &direct); err == nil {
		if text := strings.TrimSpace(direct.Best.Text); text != "" {
			return text
		}
		if text := strings.TrimSpace(direct.Text); text != "" {
			return text
		}
		if text := extractOCRTextFromRawJSON(direct.Best.Detail); text != "" {
			return text
		}
		if text := extractOCRTextFromRawJSON(direct.Detail); text != "" {
			return text
		}
	}

	var combined struct {
		Detail []struct {
			Detail json.RawMessage `json:"detail"`
			Text   string          `json:"text"`
		} `json:"detail"`
	}
	if err := json.Unmarshal([]byte(detailJSON), &combined); err == nil {
		for _, item := range combined.Detail {
			if text := strings.TrimSpace(item.Text); text != "" {
				return text
			}
			if text := extractOCRTextFromRawJSON(item.Detail); text != "" {
				return text
			}
		}
	}

	var combinedArray []struct {
		Detail json.RawMessage `json:"detail"`
		Text   string          `json:"text"`
	}
	if err := json.Unmarshal([]byte(detailJSON), &combinedArray); err == nil {
		for _, item := range combinedArray {
			if text := strings.TrimSpace(item.Text); text != "" {
				return text
			}
			if text := extractOCRTextFromRawJSON(item.Detail); text != "" {
				return text
			}
		}
	}

	return ""
}

func extractOCRTextFromRawJSON(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}

	var detailString string
	if err := json.Unmarshal(raw, &detailString); err == nil {
		return extractOCRTextFromDetailJSON(detailString)
	}

	return extractOCRTextFromDetailJSON(string(raw))
}

type ocrFragment struct {
	text string
	x    int
	y    int
	w    int
	h    int
}

type ocrCandidate struct {
	text          string
	digitCount    int
	fragmentCount int
	priority      int
}

func buildOCRCandidate(results []*maa.RecognitionResult, priority int) ocrCandidate {
	return buildOCRCandidateFromFragments(collectOCRFragments(results), priority)
}

func buildOCRCandidateFromFragments(fragments []ocrFragment, priority int) ocrCandidate {
	uniqueFragments := uniqueOCRFragments(fragments)
	text := joinOCRFragments(uniqueFragments)

	return ocrCandidate{
		text:          text,
		digitCount:    countDigits(text),
		fragmentCount: len(uniqueFragments),
		priority:      priority,
	}
}

func collectOCRFragments(results []*maa.RecognitionResult) []ocrFragment {
	fragments := make([]ocrFragment, 0, len(results))
	for _, result := range results {
		if result == nil {
			continue
		}

		ocrResult, ok := result.AsOCR()
		if !ok {
			continue
		}

		text := strings.TrimSpace(ocrResult.Text)
		if text == "" {
			continue
		}

		fragments = append(fragments, ocrFragment{
			text: text,
			x:    ocrResult.Box.X(),
			y:    ocrResult.Box.Y(),
			w:    ocrResult.Box.Width(),
			h:    ocrResult.Box.Height(),
		})
	}

	return fragments
}

func selectOCRCandidate(candidates []ocrCandidate) ocrCandidate {
	best := ocrCandidate{}
	found := false
	for _, candidate := range candidates {
		if candidate.text == "" {
			continue
		}
		if !found || betterOCRCandidate(candidate, best) {
			best = candidate
			found = true
		}
	}

	return best
}

func betterOCRCandidate(candidate ocrCandidate, current ocrCandidate) bool {
	if candidate.digitCount != current.digitCount {
		return candidate.digitCount > current.digitCount
	}
	if candidate.fragmentCount != current.fragmentCount {
		return candidate.fragmentCount < current.fragmentCount
	}

	return candidate.priority < current.priority
}

func countDigits(text string) int {
	count := 0
	for _, r := range text {
		if r >= '0' && r <= '9' {
			count++
		}
	}

	return count
}

func uniqueOCRFragments(fragments []ocrFragment) []ocrFragment {
	if len(fragments) == 0 {
		return nil
	}

	unique := make([]ocrFragment, 0, len(fragments))
	seen := make(map[string]struct{}, len(fragments))
	for _, fragment := range fragments {
		fragment.text = strings.TrimSpace(fragment.text)
		if fragment.text == "" {
			continue
		}

		key := fmt.Sprintf("%d:%d:%d:%d:%s", fragment.x, fragment.y, fragment.w, fragment.h, fragment.text)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		unique = append(unique, fragment)
	}

	sort.SliceStable(unique, func(i, j int) bool {
		if unique[i].y != unique[j].y {
			return unique[i].y < unique[j].y
		}
		return unique[i].x < unique[j].x
	})

	return unique
}

func joinOCRFragments(fragments []ocrFragment) string {
	if len(fragments) == 0 {
		return ""
	}

	var builder strings.Builder
	for _, fragment := range fragments {
		builder.WriteString(fragment.text)
	}

	return builder.String()
}
