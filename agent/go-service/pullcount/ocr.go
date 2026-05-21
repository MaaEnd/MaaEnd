package pullcount

import (
	"encoding/json"
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

// readTitleFromRecognition extracts and cleans the first non-empty item title from Pipeline OCR.
func readTitleFromRecognition(detail *maa.RecognitionDetail) (string, bool) {
	if detail == nil || !detail.Hit {
		return "", false
	}
	for _, text := range ocrTextCandidates(detail) {
		if cleaned := cleanTitleText(text); cleaned != "" {
			return cleaned, true
		}
	}
	return "", false
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

	appendOCRResults(detail, appendText)
	for _, text := range detailJSONOCRTexts(detail.DetailJson) {
		appendText(text)
	}
	for _, child := range detail.CombinedResult {
		for _, text := range ocrTextCandidates(child) {
			appendText(text)
		}
	}
	return texts
}

// appendOCRResults appends OCR text from MaaFramework parsed recognition results.
func appendOCRResults(detail *maa.RecognitionDetail, appendText func(string)) {
	if detail == nil || detail.Results == nil {
		return
	}

	appendResult := func(result *maa.RecognitionResult) {
		if result == nil {
			return
		}
		ocrResult, ok := result.AsOCR()
		if !ok {
			return
		}
		appendText(ocrResult.Text)
	}

	appendResult(detail.Results.Best)
	for _, source := range [][]*maa.RecognitionResult{detail.Results.Filtered, detail.Results.All} {
		for _, result := range source {
			appendResult(result)
		}
	}
}

// detailJSONOCRTexts parses OCR text from raw detail JSON when tests or diagnostics provide it directly.
func detailJSONOCRTexts(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}

	var payload struct {
		Text string `json:"text"`
		Best *struct {
			Text string `json:"text"`
		} `json:"best"`
		Filtered []struct {
			Text string `json:"text"`
		} `json:"filtered"`
		All []struct {
			Text string `json:"text"`
		} `json:"all"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return nil
	}

	texts := make([]string, 0)
	if payload.Text != "" {
		texts = append(texts, payload.Text)
	}
	if payload.Best != nil {
		texts = append(texts, payload.Best.Text)
	}
	for _, item := range payload.Filtered {
		texts = append(texts, item.Text)
	}
	for _, item := range payload.All {
		texts = append(texts, item.Text)
	}
	return texts
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
		if started && isNumberSeparator(r) {
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

// isNumberSeparator reports whether a rune is a thousands separator inside OCR text.
func isNumberSeparator(r rune) bool {
	return r == ','
}

// cleanTitleText removes common OCR decorations while keeping the item name.
func cleanTitleText(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	fields := strings.Fields(text)
	for _, field := range fields {
		if normalizeName(field) != "" {
			return strings.Trim(field, "[]|")
		}
	}
	return strings.Trim(text, "[]|")
}

// ignoredPageTitle reports UI labels that are not warehouse item titles.
func ignoredPageTitle(text string) bool {
	switch normalizeName(text) {
	case "", normalizeName("珍贵物品"), normalizeName("貴重品"), normalizeName("Precious Items"):
		return true
	default:
		return false
	}
}
