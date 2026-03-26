package autostockpile

import (
	"encoding/json"
	"regexp"
	"strings"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

var priceRe = regexp.MustCompile(`^(\d{3,4})$`)

type ocrTextPolicy int

const (
	ocrTextPolicyFilteredOnly ocrTextPolicy = iota
	ocrTextPolicyBestFilteredAll
)

type templateHitStrategy int

const (
	templateHitTopmost templateHitStrategy = iota
	templateHitLowest
)

func extractCustomRecognitionDetailJSON(detail *maa.RecognitionDetail) string {
	if detail == nil || detail.DetailJson == "" {
		return ""
	}

	var wrapped struct {
		Best struct {
			Detail json.RawMessage `json:"detail"`
		} `json:"best"`
	}
	if err := json.Unmarshal([]byte(detail.DetailJson), &wrapped); err == nil && len(wrapped.Best.Detail) > 0 {
		return rawJSONToString(wrapped.Best.Detail)
	}

	return detail.DetailJson
}

func filteredRecognitionResults(detail *maa.RecognitionDetail) []*maa.RecognitionResult {
	if detail == nil || detail.Results == nil {
		return nil
	}
	if len(detail.Results.Filtered) > 0 {
		return detail.Results.Filtered
	}
	return nil
}

func filteredOCRCandidates(detail *maa.RecognitionDetail) []*maa.OCRResult {
	results := filteredRecognitionResults(detail)
	if len(results) == 0 {
		return nil
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

func ocrTextCandidates(detail *maa.RecognitionDetail, policy ocrTextPolicy) []string {
	var sources [][]*maa.RecognitionResult
	switch policy {
	case ocrTextPolicyFilteredOnly:
		sources = [][]*maa.RecognitionResult{filteredRecognitionResults(detail)}
	case ocrTextPolicyBestFilteredAll:
		if detail != nil && detail.Results != nil {
			sources = [][]*maa.RecognitionResult{
				resultsFromBest(detail.Results.Best),
				detail.Results.Filtered,
				detail.Results.All,
			}
		}
	}

	texts := make([]string, 0)
	seen := make(map[string]struct{})
	for _, source := range sources {
		for _, result := range source {
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
			if _, exists := seen[text]; exists {
				continue
			}
			seen[text] = struct{}{}
			texts = append(texts, text)
		}
	}

	return texts
}

func firstOCRText(detail *maa.RecognitionDetail) (string, bool) {
	texts := ocrTextCandidates(detail, ocrTextPolicyBestFilteredAll)
	if len(texts) == 0 {
		return "", false
	}
	return texts[0], true
}

func pickTemplateHit(detail *maa.RecognitionDetail, strategy templateHitStrategy) (maa.Rect, bool) {
	results := filteredRecognitionResults(detail)
	if len(results) == 0 && detail != nil && detail.Results != nil && detail.Results.Best != nil {
		results = []*maa.RecognitionResult{detail.Results.Best}
	}
	if len(results) == 0 {
		return maa.Rect{}, false
	}

	selected := maa.Rect{}
	hit := false
	for _, result := range results {
		if result == nil {
			continue
		}
		tm, ok := result.AsTemplateMatch()
		if !ok {
			continue
		}

		if !hit {
			selected = tm.Box
			hit = true
			continue
		}

		switch strategy {
		case templateHitLowest:
			if tm.Box.Y() > selected.Y() {
				selected = tm.Box
			}
		default:
			if tm.Box.Y() < selected.Y() {
				selected = tm.Box
			}
		}
	}

	return selected, hit
}

func resultsFromBest(best *maa.RecognitionResult) []*maa.RecognitionResult {
	if best == nil {
		return nil
	}
	return []*maa.RecognitionResult{best}
}

func rawJSONToString(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	if raw[0] == '"' {
		var value string
		if err := json.Unmarshal(raw, &value); err != nil {
			return string(raw)
		}
		return value
	}
	return string(raw)
}
