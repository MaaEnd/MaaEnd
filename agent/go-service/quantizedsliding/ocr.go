package quantizedsliding

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

func readHitBox(detail *maa.RecognitionDetail) ([]int, bool) {
	if detail == nil {
		return nil, false
	}

	candidate := findRecognitionDetailByName(detail, nodeQuantizedSlidingSwipeButton)
	if candidate == nil {
		candidate = detail
	}

	if box, ok := readTemplateMatchBestBox(candidate); ok {
		return box, true
	}

	if len(candidate.Box) >= 4 {
		return []int{candidate.Box[0], candidate.Box[1], candidate.Box[2], candidate.Box[3]}, true
	}

	if candidate != detail && len(detail.Box) >= 4 {
		return []int{detail.Box[0], detail.Box[1], detail.Box[2], detail.Box[3]}, true
	}

	return nil, false
}

func readQuantityText(detail *maa.RecognitionDetail, concatAllFilteredDigits bool) string {
	if detail == nil {
		return ""
	}

	candidate := findRecognitionDetailByName(detail, nodeQuantizedSlidingGetQuantity)
	if candidate == nil {
		candidate = detail
	}

	if concatAllFilteredDigits {
		if text := joinFilteredOCRText(candidate.Results); text != "" {
			return text
		}
	} else {
		if text := readBestOCRText(candidate.Results); text != "" {
			return text
		}
	}

	if text := extractOCRTextFromDetailJSON(candidate.DetailJson); text != "" {
		return text
	}

	if candidate != detail {
		return extractOCRTextFromDetailJSON(detail.DetailJson)
	}

	return ""
}

func readQuantityValue(detail *maa.RecognitionDetail, concatAllFilteredDigits bool) (int, error) {
	text := readQuantityText(detail, concatAllFilteredDigits)
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

func readBestOCRText(results *maa.RecognitionResults) string {
	if results == nil || results.Best == nil {
		return ""
	}

	ocrResult, ok := results.Best.AsOCR()
	if !ok {
		return ""
	}

	return strings.TrimSpace(ocrResult.Text)
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

func readTemplateMatchBestBox(detail *maa.RecognitionDetail) ([]int, bool) {
	if detail == nil || detail.Results == nil || detail.Results.Best == nil {
		return nil, false
	}

	tm, ok := detail.Results.Best.AsTemplateMatch()
	if !ok {
		return nil, false
	}

	return []int{tm.Box.X(), tm.Box.Y(), tm.Box.Width(), tm.Box.Height()}, true
}

func joinFilteredOCRText(results *maa.RecognitionResults) string {
	if results == nil || len(results.Filtered) == 0 {
		return ""
	}

	fragments := make([]ocrFragment, 0, len(results.Filtered))
	for _, result := range results.Filtered {
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
		})
	}

	if len(fragments) == 0 {
		return ""
	}

	sort.SliceStable(fragments, func(i int, j int) bool {
		if fragments[i].y != fragments[j].y {
			return fragments[i].y < fragments[j].y
		}

		return fragments[i].x < fragments[j].x
	})

	var builder strings.Builder
	for _, fragment := range fragments {
		builder.WriteString(fragment.text)
	}

	return builder.String()
}

type ocrFragment struct {
	text string
	x    int
	y    int
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
