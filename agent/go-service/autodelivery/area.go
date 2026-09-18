package autodelivery

import (
	"fmt"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

// AreaResolution 是按区域 OCR 文本匹配到的仓储节点。
type AreaResolution struct {
	// ID 是 MaaEnd 侧仓储节点 ID，可直接拼接业务节点名。
	ID string
	// DepotID 是游戏侧仓储 ID。
	DepotID string
	// Similarity 是最佳匹配的相似度。
	Similarity float64
	// RunnerUpSimilarity 是次佳匹配的相似度。
	RunnerUpSimilarity float64
}

// AreaTextFromRecognition 从识别结果中取出区域节点（AutoDeliveryCheckAreaText）的 OCR 文本。
func AreaTextFromRecognition(detail *maa.RecognitionDetail) (string, error) {
	areaDetail := findRecognitionDetail(detail, areaTextNode)
	if areaDetail == nil {
		return "", fmt.Errorf("delivery area OCR detail is missing")
	}
	return recognitionText(areaDetail)
}

// ResolveAreaFromText 按区域 OCR 文本匹配仓储节点。
func ResolveAreaFromText(areaText string) (AreaResolution, error) {
	areas, err := getAreas()
	if err != nil {
		return AreaResolution{}, err
	}
	area, match, err := resolveArea(areaText, areas)
	if err != nil {
		return AreaResolution{
			Similarity:         match.Similarity,
			RunnerUpSimilarity: match.RunnerUpSimilarity,
		}, err
	}
	return AreaResolution{
		ID:                 area.ID,
		DepotID:            area.DepotID,
		Similarity:         match.Similarity,
		RunnerUpSimilarity: match.RunnerUpSimilarity,
	}, nil
}
