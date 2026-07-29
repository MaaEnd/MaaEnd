package ims

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

var (
	ocrNumericPattern  = regexp.MustCompile(`(?i)[+-]?(?:\d+(?:[.,]\d+)?|[.,]\d+)\s*(?:[a-z]+|万|亿)?`)
	asciiLetterPattern = regexp.MustCompile(`[A-Za-z]+$`)
)

func extractOCRQuantity(detail *maa.RecognitionDetail) (int, error) {
	if detail == nil || detail.Results == nil {
		return 0, fmt.Errorf("recognition detail is empty")
	}
	if best := detail.Results.Best; best != nil {
		if ocrResult, ok := best.AsOCR(); ok {
			return parseOCRNumericValue(ocrResult.Text)
		}
	}
	for _, result := range detail.Results.All {
		if ocrResult, ok := result.AsOCR(); ok {
			return parseOCRNumericValue(ocrResult.Text)
		}
	}
	return 0, fmt.Errorf("no ocr result found")
}

func parseOCRNumericValue(text string) (int, error) {
	cleaned := strings.TrimSpace(text)
	if cleaned == "" {
		return 0, fmt.Errorf("ocr text is empty")
	}
	matchIndex := ocrNumericPattern.FindStringIndex(cleaned)
	if matchIndex == nil {
		return 0, fmt.Errorf("ocr text %q contains no numeric value", cleaned)
	}
	match := cleaned[matchIndex[0]:matchIndex[1]]
	numberText, multiplier, err := normalizeOCRNumericToken(match)
	if err != nil {
		return 0, err
	}
	value, err := strconv.ParseFloat(numberText, 64)
	if err != nil {
		return 0, err
	}
	scaled := math.Round(value * multiplier)
	return int(scaled), nil
}

func normalizeOCRNumericToken(token string) (string, float64, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return "", 0, fmt.Errorf("numeric token is empty")
	}
	multiplier := 1.0
	if strings.HasSuffix(token, "万") {
		multiplier = 1e4
		token = strings.TrimSuffix(token, "万")
	} else if strings.HasSuffix(token, "亿") {
		multiplier = 1e8
		token = strings.TrimSuffix(token, "亿")
	} else if loc := asciiLetterPattern.FindStringIndex(token); loc != nil {
		unit := strings.ToLower(token[loc[0]:loc[1]])
		token = strings.TrimSpace(token[:loc[0]])
		switch unit {
		case "k":
			multiplier = 1e3
		case "m":
			multiplier = 1e6
		case "b":
			multiplier = 1e9
		default:
			return "", 0, fmt.Errorf("unsupported numeric unit %q", unit)
		}
	}
	token = strings.TrimSpace(token)
	token = strings.ReplaceAll(token, ",", "")
	if token == "" {
		return "", 0, fmt.Errorf("numeric token missing digits")
	}
	return token, multiplier, nil
}
