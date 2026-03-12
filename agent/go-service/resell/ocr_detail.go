package resell

import (
	"encoding/json"

	"github.com/MaaXYZ/maa-framework-go/v4"
)

func extractOCRText(detail *maa.RecognitionDetail) string {
	if detail == nil {
		return ""
	}
	if detail.Results != nil {
		for _, results := range [][]*maa.RecognitionResult{
			{detail.Results.Best},
			detail.Results.Filtered,
			detail.Results.All,
		} {
			if len(results) > 0 && results[0] != nil {
				if ocrResult, ok := results[0].AsOCR(); ok && ocrResult.Text != "" {
					return ocrResult.Text
				}
			}
		}
	}
	// Or/And 节点：Results 为 nil，子节点在 CombinedResult 中（如 Or 包裹 OCR）
	if len(detail.CombinedResult) > 0 {
		for _, child := range detail.CombinedResult {
			if text := extractOCRText(child); text != "" {
				return text
			}
		}
	}
	if detail.DetailJson != "" {
		if text, _, _, _, _ := extractOCRFromDetailJson(detail.DetailJson); text != "" {
			return text
		}
	}
	return ""
}

// extractOCRFromDetailJson 从 Or/OCR 的 DetailJson 提取 text 和 box（用于 Or 识别时 Results 无直接 OCR 的兜底）
func extractOCRFromDetailJson(detailJson string) (text string, boxX, boxY, boxW, boxH int) {
	// 尝试 Or 结构：detail 为数组，首个子项含 detail.best
	var orStruct struct {
		Detail []struct {
			Detail struct {
				Best struct {
					Text string `json:"text"`
					Box  []int  `json:"box"`
				} `json:"best"`
			} `json:"detail"`
		} `json:"detail"`
	}
	if err := json.Unmarshal([]byte(detailJson), &orStruct); err == nil && len(orStruct.Detail) > 0 {
		b := orStruct.Detail[0].Detail.Best
		if b.Text != "" && len(b.Box) >= 4 {
			return b.Text, b.Box[0], b.Box[1], b.Box[2], b.Box[3]
		}
	}
	// 尝试直接 OCR 结构：best 含 text 和 box
	var ocrStruct struct {
		Best struct {
			Text string `json:"text"`
			Box  []int  `json:"box"`
		} `json:"best"`
	}

	if err := json.Unmarshal([]byte(detailJson), &ocrStruct); err == nil && ocrStruct.Best.Text != "" && len(ocrStruct.Best.Box) >= 4 {
		b := ocrStruct.Best.Box
		return ocrStruct.Best.Text, b[0], b[1], b[2], b[3]
	}
	return "", 0, 0, 0, 0
}
