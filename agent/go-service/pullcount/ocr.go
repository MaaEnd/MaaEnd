package pullcount

import (
	"fmt"
	"strconv"
	"strings"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

// --- OCR Detail Reading --- //

// readIntegerFromRecognition extracts the first integer-like OCR value from Pipeline recognition detail.
func readIntegerFromRecognition(detail *maa.RecognitionDetail) (int, error) {
	if detail == nil || !detail.Hit {
		return 0, fmt.Errorf("OCR not hit")
	}
	for _, text := range ocrTextCandidates(detail) {
		value, err := parseIntegerText(text)
		if err == nil {
			return value, nil
		}
	}
	return 0, fmt.Errorf("no integer OCR result")
}

// ocrTextCandidates returns OCR texts in preferred reading order.
func ocrTextCandidates(detail *maa.RecognitionDetail) []string {
	texts := make([]string, 0)
	seen := make(map[string]struct{})
	appendText := func(text string) {
		text = strings.TrimSpace(text)
		if text == "" {
			return
		}
		if _, exists := seen[text]; exists {
			return
		}
		seen[text] = struct{}{}
		texts = append(texts, text)
	}

	appendOCRTexts(detail.Results, appendText)
	for _, child := range detail.CombinedResult {
		for _, text := range ocrTextCandidates(child) {
			appendText(text)
		}
	}
	return texts
}

// appendOCRTexts appends OCR text from MaaFramework parsed recognition results.
func appendOCRTexts(results *maa.RecognitionResults, appendText func(string)) {
	if results == nil {
		return
	}

	appendResult := func(result *maa.RecognitionResult) {
		if result == nil {
			return
		}
		ocrResult, ok := result.AsOCR()
		if ok {
			appendText(ocrResult.Text)
		}
	}

	appendResult(results.Best)
	for _, source := range [][]*maa.RecognitionResult{results.Filtered, results.All} {
		for _, result := range source {
			appendResult(result)
		}
	}
}

// parseIntegerText extracts the first decimal counter from OCR text.
func parseIntegerText(text string) (int, error) {
	var b strings.Builder
	started := false
	for _, r := range text {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
			started = true
			continue
		}
		if started && r == ',' {
			continue
		}
		if started {
			break
		}
	}
	digits := b.String()
	if digits == "" {
		return 0, fmt.Errorf("no digits in %q", text)
	}
	return strconv.Atoi(digits)
}
