package bettersliding

import (
	"fmt"
	"strconv"
	"strings"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

func readHitBox(detail *maa.RecognitionDetail) ([]int, bool) {
	if detail == nil {
		return nil, false
	}

	candidate := findRecognitionDetailByName(detail, nodeBetterSlidingSwipeButton)
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

func readQuantityText(detail *maa.RecognitionDetail) string {
	if detail == nil {
		return ""
	}

	candidate := findRecognitionDetailByName(detail, nodeBetterSlidingGetQuantity)
	if candidate == nil {
		candidate = detail
	}

	return readBestOCRText(candidate.Results)
}

func readQuantityValue(detail *maa.RecognitionDetail) (int, error) {
	text := readQuantityText(detail)
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
