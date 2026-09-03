package autodelivery

import (
	"fmt"
	"strings"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

func destinationOCRFields(detail *maa.RecognitionDetail) (areaText, destinationText string, ok bool) {
	areaDetail := findRecognitionDetail(detail, areaTextNode)
	destinationDetail := findRecognitionDetail(detail, destinationTextNode)
	if areaDetail == nil || destinationDetail == nil {
		return "", "", false
	}

	areaText, _ = recognitionText(areaDetail)
	destinationText, _ = recognitionText(destinationDetail)
	return areaText, destinationText, true
}

func findRecognitionDetail(detail *maa.RecognitionDetail, name string) *maa.RecognitionDetail {
	if detail == nil {
		return nil
	}
	if detail.Name == name {
		return detail
	}
	for _, child := range detail.CombinedResult {
		if found := findRecognitionDetail(child, name); found != nil {
			return found
		}
	}
	return nil
}

func recognitionText(detail *maa.RecognitionDetail) (string, error) {
	if detail == nil || detail.Results == nil {
		return "", fmt.Errorf("delivery objective OCR returned no recognition results")
	}

	results := detail.Results.Filtered
	if len(results) == 0 {
		results = detail.Results.All
	}
	if len(results) == 0 && detail.Results.Best != nil {
		results = []*maa.RecognitionResult{detail.Results.Best}
	}

	var text strings.Builder
	for _, result := range results {
		if result == nil {
			continue
		}
		ocr, ok := result.AsOCR()
		if !ok || ocr == nil {
			continue
		}
		text.WriteString(strings.TrimSpace(ocr.Text))
	}
	if text.Len() == 0 {
		return "", fmt.Errorf("delivery objective OCR returned no text")
	}
	return text.String(), nil
}
