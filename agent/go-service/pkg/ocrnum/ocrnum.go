// Package ocrnum parses OCR numeric text with magnitude suffixes
// (k/m/b, 万/萬/만, 亿/億/억).
package ocrnum

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

var (
	ocrNumericPattern  = regexp.MustCompile(`(?i)[+-]?(?:\d+(?:[.,]\d+)?|[.,]\d+)\s*(?:[a-z]+|万|萬|亿|億|만|억)?`)
	asciiLetterPattern = regexp.MustCompile(`[A-Za-z]+$`)
)

// Parse converts OCR text like "1.8k", "12m", or "1.38万" into an int.
func Parse(text string) (int, error) {
	cleaned := strings.TrimSpace(text)
	if cleaned == "" {
		return 0, fmt.Errorf("ocr text is empty")
	}

	matchIndex := ocrNumericPattern.FindStringIndex(cleaned)
	if matchIndex == nil {
		return 0, fmt.Errorf("ocr text %q contains no numeric value", cleaned)
	}
	match := cleaned[matchIndex[0]:matchIndex[1]]

	numberText, multiplier, err := normalizeToken(match)
	if err != nil {
		return 0, err
	}

	value, err := strconv.ParseFloat(numberText, 64)
	if err != nil {
		return 0, err
	}

	scaled := math.Round(value * multiplier)
	if scaled > float64(math.MaxInt) || scaled < float64(math.MinInt) {
		clamped := clampInt(scaled)
		log.Warn().
			Str("component", "ocrnum").
			Str("ocr_text", cleaned).
			Float64("raw_value", scaled).
			Int("clamped_value", clamped).
			Msg("ocr numeric value out of int range, clamped")
		return clamped, nil
	}

	return int(scaled), nil
}

// Extract parses a numeric quantity from an OCR recognition detail.
func Extract(detail *maa.RecognitionDetail) (int, error) {
	if detail == nil || detail.Results == nil {
		return 0, fmt.Errorf("recognition detail is empty")
	}
	if best := detail.Results.Best; best != nil {
		if ocrResult, ok := best.AsOCR(); ok {
			return Parse(ocrResult.Text)
		}
	}
	for _, result := range detail.Results.All {
		if ocrResult, ok := result.AsOCR(); ok {
			return Parse(ocrResult.Text)
		}
	}
	return 0, fmt.Errorf("no ocr result found")
}

func normalizeToken(token string) (string, float64, error) {
	normalized := strings.TrimSpace(token)
	if normalized == "" {
		return "", 0, fmt.Errorf("ocr numeric token is empty")
	}

	multiplier := 1.0
	for _, suffix := range []struct {
		unit       string
		multiplier float64
	}{
		{unit: "億", multiplier: 1e8},
		{unit: "억", multiplier: 1e8},
		{unit: "亿", multiplier: 1e8},
		{unit: "萬", multiplier: 1e4},
		{unit: "만", multiplier: 1e4},
		{unit: "万", multiplier: 1e4},
		{unit: "K", multiplier: 1e3},
		{unit: "k", multiplier: 1e3},
		{unit: "M", multiplier: 1e6},
		{unit: "m", multiplier: 1e6},
		{unit: "B", multiplier: 1e9},
		{unit: "b", multiplier: 1e9},
	} {
		if strings.HasSuffix(normalized, suffix.unit) {
			normalized = strings.TrimSpace(strings.TrimSuffix(normalized, suffix.unit))
			multiplier = suffix.multiplier
			break
		}
	}

	if unsupportedSuffix := asciiLetterPattern.FindString(normalized); unsupportedSuffix != "" {
		return "", 0, fmt.Errorf("unsupported ocr numeric suffix %q in %q", unsupportedSuffix, token)
	}

	if normalized == "" {
		return "", 0, fmt.Errorf("ocr numeric token %q has no numeric part", token)
	}

	normalized = strings.ReplaceAll(normalized, " ", "")
	if strings.Contains(normalized, ".") {
		normalized = strings.ReplaceAll(normalized, ",", "")
	} else if strings.Count(normalized, ",") == 1 {
		parts := strings.Split(normalized, ",")
		if len(parts) == 2 && len(parts[1]) != 3 {
			normalized = parts[0] + "." + parts[1]
		} else {
			normalized = strings.ReplaceAll(normalized, ",", "")
		}
	} else {
		normalized = strings.ReplaceAll(normalized, ",", "")
	}

	return normalized, multiplier, nil
}

func clampInt(value float64) int {
	if value > float64(math.MaxInt) {
		return math.MaxInt
	}
	if value < float64(math.MinInt) {
		return math.MinInt
	}
	return int(value)
}
