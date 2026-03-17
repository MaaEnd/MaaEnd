package quantizedsliding

import (
	"encoding/json"
	"fmt"
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

	for _, group := range [][]*maa.RecognitionResult{{results.Best}, results.Filtered, results.All} {
		for _, result := range group {
			if result == nil {
				continue
			}

			ocrResult, ok := result.AsOCR()
			if !ok {
				continue
			}

			text := strings.TrimSpace(ocrResult.Text)
			if text != "" {
				return text
			}
		}
	}

	return ""
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
