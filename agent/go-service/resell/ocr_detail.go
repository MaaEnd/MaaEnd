package resell

import (
	"github.com/MaaXYZ/maa-framework-go/v4"
)

// extractOCRText 从 RecognitionDetail 中提取 OCR 文本。
// 优先级：Best > Filtered > All > CombinedResult（递归）
func extractOCRText(detail *maa.RecognitionDetail) string {
	if detail == nil {
		return ""
	}

	// 优先从 Results 中查找
	if detail.Results != nil {
		// 按优先级顺序尝试获取 OCR 文本
		if text := tryGetOCRText(detail.Results.Best); text != "" {
			return text
		}
		if text := tryGetOCRTexts(detail.Results.Filtered); text != "" {
			return text
		}
		if text := tryGetOCRTexts(detail.Results.All); text != "" {
			return text
		}
	}

	// 递归查找 CombinedResult（Or/And 节点）
	for _, child := range detail.CombinedResult {
		if text := extractOCRText(child); text != "" {
			return text
		}
	}

	return ""
}

// tryGetOCRText 尝试从单个 RecognitionResult 获取 OCR 文本
func tryGetOCRText(result *maa.RecognitionResult) string {
	if result == nil {
		return ""
	}
	if ocrResult, ok := result.AsOCR(); ok {
		return ocrResult.Text
	}
	return ""
}

// tryGetOCRTexts 尝试从 RecognitionResult 切片获取 OCR 文本（返回第一个非空结果）
func tryGetOCRTexts(results []*maa.RecognitionResult) string {
	for _, result := range results {
		if text := tryGetOCRText(result); text != "" {
			return text
		}
	}
	return ""
}
