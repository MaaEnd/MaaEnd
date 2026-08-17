package iconrecognition

import (
	"encoding/json"
	"fmt"
	"strings"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

// ErrorCode 是 IconRecognition 结构化错误的稳定标识。
type ErrorCode string

const (
	ErrorCodeInvalidImage        ErrorCode = "invalid_image"
	ErrorCodeInvalidArgument     ErrorCode = "invalid_argument"
	ErrorCodeNoMatch             ErrorCode = "no_match"
	ErrorCodeGridDetectionFailed ErrorCode = "grid_detection_failed"
	ErrorCodeException           ErrorCode = "exception"
)

// DetailError 是 IconRecognition 未命中或失败时返回的结构化错误。
type DetailError struct {
	Code    ErrorCode `json:"code"`
	Message string    `json:"message"`
}

// Match 是 IconRecognition 单个候选物品的识别结果。
type Match struct {
	ItemID       string   `json:"item_id"`
	Name         string   `json:"name"`
	Category     string   `json:"category"`
	StorageKind  string   `json:"storage_kind"`
	CategoryType string   `json:"category_type"`
	Rarity       int      `json:"rarity"`
	CellBox      maa.Rect `json:"cell_box"`
	ItemBox      maa.Rect `json:"item_box"`
	Score        float64  `json:"score"`
	Row          *int     `json:"row,omitempty"`
	Column       *int     `json:"column,omitempty"`
}

// Detail 是 IconRecognition custom recognition 返回的 detail JSON。
type Detail struct {
	DetailVersion int          `json:"detail_version"`
	Matched       bool         `json:"matched"`
	GridType      GridType     `json:"grid_type"`
	ROI           maa.Rect     `json:"roi"`
	Matches       []Match      `json:"matches"`
	Error         *DetailError `json:"error,omitempty"`
}

// ParseDetail 解析 IconRecognition 返回的 detail JSON。
func ParseDetail(raw string) (Detail, error) {
	var detail Detail
	if strings.TrimSpace(raw) == "" {
		return detail, fmt.Errorf("IconRecognition detail is empty")
	}
	if err := json.Unmarshal([]byte(raw), &detail); err != nil {
		return detail, fmt.Errorf("parse IconRecognition detail: %w", err)
	}
	return detail, nil
}

// ParseRecognitionDetail 从 Maa Custom Recognition 结果中解析 IconRecognition detail。
// 命中时使用 Best，未命中时使用 All 的首项；Custom Recognition 不需要合并结果桶。
func ParseRecognitionDetail(detail *maa.RecognitionDetail) (Detail, string, error) {
	if detail == nil || detail.Results == nil {
		return Detail{}, "", fmt.Errorf("IconRecognition recognition detail is empty")
	}

	result := detail.Results.Best
	if result == nil && len(detail.Results.All) > 0 {
		result = detail.Results.All[0]
	}
	if result == nil {
		return Detail{}, "", fmt.Errorf("IconRecognition custom result is empty")
	}

	custom, ok := result.AsCustom()
	if !ok || custom == nil {
		return Detail{}, "", fmt.Errorf("IconRecognition result is not custom recognition")
	}
	parsed, err := ParseDetail(custom.Detail)
	if err != nil {
		return Detail{}, "", err
	}
	return parsed, custom.Detail, nil
}
