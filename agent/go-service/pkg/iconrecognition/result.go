package iconrecognition

import (
	"encoding/json"
	"fmt"

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

// Error 返回带稳定错误码的诊断信息。
func (detail *DetailError) Error() string {
	return fmt.Sprintf("IconRecognition %s: %s", detail.Code, detail.Message)
}

// Alias 是与代表物品共用同一组图标的候选物品。
type Alias struct {
	ItemID string `json:"item_id"`
	Name   string `json:"name"`
}

// Match 是 IconRecognition 单个候选物品的识别结果。
type Match struct {
	ItemID       string `json:"item_id"`
	Name         string `json:"name"`
	Category     string `json:"category"`
	StorageKind  string `json:"storage_kind"`
	CategoryType string `json:"category_type"`
	Rarity       int    `json:"rarity"`
	// Aliases 仅包含与代表物品共享图标身份的候选物品。
	Aliases []Alias  `json:"aliases,omitempty"`
	CellBox maa.Rect `json:"cell_box"`
	ItemBox maa.Rect `json:"item_box"`
	Score   float64  `json:"score"`
	// RegionUnavailable 仅标记当前地区无法使用的物品；false 没有必要导出。
	RegionUnavailable bool `json:"region_unavailable,omitempty"`
	Row               *int `json:"row,omitempty"`
	Column            *int `json:"column,omitempty"`
}

// Detail 是单个 Custom 结果的物品详情，失败时只携带 Error。
type Detail struct {
	Match
	Error *DetailError `json:"error,omitempty"`
}

// Results 是框架返回的有序结果集合，读取时不自动转换详情。
type Results []*maa.RecognitionResult

// Matches 按需解析每项物品详情，保留结果顺序；失败详情以 DetailError 返回。
func (results Results) Matches() ([]Match, error) {
	matches := make([]Match, 0, len(results))
	for index, result := range results {
		if result == nil {
			return nil, fmt.Errorf("IconRecognition result %d is nil", index)
		}
		custom, ok := result.AsCustom()
		if !ok || custom == nil {
			return nil, fmt.Errorf("IconRecognition result %d is not custom recognition", index)
		}
		var detail Detail
		if err := json.Unmarshal([]byte(custom.Detail), &detail); err != nil {
			return nil, fmt.Errorf("parse IconRecognition result %d: %w", index, err)
		}
		if detail.Error != nil {
			return nil, detail.Error
		}
		if detail.ItemID == "" {
			return nil, fmt.Errorf("IconRecognition result %d has no item_id", index)
		}
		matches = append(matches, detail.Match)
	}
	return matches, nil
}

// RecognitionDetail 是框架识别结果的轻量封装，不解析或汇总结果详情。
type RecognitionDetail struct {
	base *maa.RecognitionDetail
}

// NewRecognitionDetail 封装 IconRecognition 的框架结果，实际转换由 Matches 执行。
func NewRecognitionDetail(detail *maa.RecognitionDetail) (RecognitionDetail, error) {
	if detail == nil || detail.Results == nil {
		return RecognitionDetail{}, fmt.Errorf("IconRecognition recognition detail is empty")
	}
	if detail.Algorithm != string(maa.RecognitionTypeCustom) {
		return RecognitionDetail{}, fmt.Errorf("IconRecognition algorithm is not custom recognition")
	}
	return RecognitionDetail{base: detail}, nil
}

// All 返回框架的全部结果，不回退到其它结果集合。
func (detail RecognitionDetail) All() Results {
	if detail.base == nil {
		return nil
	}
	return Results(detail.base.Results.All)
}

// Filter 返回框架筛选后的结果。
func (detail RecognitionDetail) Filter() Results {
	if detail.base == nil {
		return nil
	}
	return Results(detail.base.Results.Filtered)
}

// Best 返回框架选中的单项集合；未命中时为空，可继续调用 Matches。
func (detail RecognitionDetail) Best() Results {
	if detail.base == nil || detail.base.Results.Best == nil {
		return nil
	}
	return Results{detail.base.Results.Best}
}

// Base 返回未经转换的原始框架识别结果。
func (detail RecognitionDetail) Base() *maa.RecognitionDetail {
	return detail.base
}
