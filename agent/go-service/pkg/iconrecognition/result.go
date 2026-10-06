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

// LegacyDetail 是旧版成功时聚合在单条 detail 里的结构（ParseRecognitionDetail 仍返回此类型）。
type LegacyDetail struct {
	DetailVersion int          `json:"detail_version"`
	Matched       bool         `json:"matched"`
	GridType      GridType     `json:"grid_type"`
	ROI           maa.Rect     `json:"roi"`
	Matches       []Match      `json:"matches"`
	Error         *DetailError `json:"error,omitempty"`
}

// Results 是 MaaFramework 返回的一组 Custom 识别结果。
type Results []*maa.RecognitionResult

// RecognitionDetail 封装 IconRecognition 的 Maa 识别详情。
type RecognitionDetail struct {
	base *maa.RecognitionDetail
}

// NewRecognitionDetail 解析 IconRecognition 的 Maa 识别详情，供 Matches 按需展开。
func NewRecognitionDetail(detail *maa.RecognitionDetail) (RecognitionDetail, error) {
	if detail == nil || detail.Results == nil {
		return RecognitionDetail{}, fmt.Errorf("IconRecognition recognition detail is empty")
	}
	if detail.Algorithm != string(maa.RecognitionTypeCustom) {
		return RecognitionDetail{}, fmt.Errorf("IconRecognition algorithm is not custom recognition")
	}
	return RecognitionDetail{base: detail}, nil
}

// All 返回 MaaFramework 保存的全部结果。
func (detail RecognitionDetail) All() Results {
	if detail.base == nil {
		return nil
	}
	return Results(detail.base.Results.All)
}

// Filter 返回 MaaFramework 筛选后的结果。
func (detail RecognitionDetail) Filter() Results {
	if detail.base == nil {
		return nil
	}
	return Results(detail.base.Results.Filtered)
}

// Best 返回 MaaFramework 根据 index 选中的结果。
func (detail RecognitionDetail) Best() Results {
	if detail.base == nil || detail.base.Results.Best == nil {
		return nil
	}
	return Results{detail.base.Results.Best}
}

// Base 返回原始 MaaFramework RecognitionDetail。
func (detail RecognitionDetail) Base() *maa.RecognitionDetail {
	return detail.base
}

// Matches 按需解析每个结果中的物品详情，并保留 MaaFramework 返回的顺序。
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
		parsed, err := parseCustomDetailMatches(custom.Detail)
		if err != nil {
			return nil, fmt.Errorf("parse IconRecognition result %d: %w", index, err)
		}
		matches = append(matches, parsed...)
	}
	return matches, nil
}

// CollectMatches 按 Filter、All、Best 的优先级合并 IconRecognition 命中，并兼容旧版聚合 detail。
func CollectMatches(buckets ...Results) ([]Match, error) {
	for _, bucket := range buckets {
		if len(bucket) == 0 {
			continue
		}
		matches, err := bucket.Matches()
		if err != nil {
			return nil, err
		}
		if len(matches) > 0 {
			return matches, nil
		}
	}
	return nil, nil
}

func parseCustomDetailMatches(raw string) ([]Match, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, fmt.Errorf("IconRecognition detail is empty")
	}
	var legacy LegacyDetail
	if err := json.Unmarshal([]byte(raw), &legacy); err != nil {
		return nil, fmt.Errorf("unmarshal IconRecognition detail: %w", err)
	}
	if legacy.Error != nil {
		return nil, legacy.Error
	}
	if len(legacy.Matches) > 0 {
		return legacy.Matches, nil
	}
	var single Match
	if err := json.Unmarshal([]byte(raw), &single); err != nil {
		return nil, fmt.Errorf("unmarshal IconRecognition match: %w", err)
	}
	if single.ItemID == "" {
		return nil, nil
	}
	return []Match{single}, nil
}

// ParseDetail 解析 IconRecognition 返回的 detail JSON（兼容旧版聚合结构）。
func ParseDetail(raw string) (LegacyDetail, error) {
	var detail LegacyDetail
	if strings.TrimSpace(raw) == "" {
		return detail, fmt.Errorf("IconRecognition detail is empty")
	}
	if err := json.Unmarshal([]byte(raw), &detail); err != nil {
		return detail, fmt.Errorf("parse IconRecognition detail: %w", err)
	}
	return detail, nil
}

// ParseRecognitionDetail 从 Maa Custom Recognition 结果中解析 IconRecognition 命中（兼容旧调用方）。
func ParseRecognitionDetail(detail *maa.RecognitionDetail) (LegacyDetail, string, error) {
	parsed, err := NewRecognitionDetail(detail)
	if err != nil {
		return LegacyDetail{}, "", err
	}
	matches, err := CollectMatches(parsed.Filter(), parsed.All(), parsed.Best())
	if err != nil {
		return LegacyDetail{}, "", err
	}
	if len(matches) == 0 {
		// 未命中时 detail 可能只在 All[0] 中携带 error。
		if bucket := parsed.All(); len(bucket) > 0 {
			if custom, ok := bucket[0].AsCustom(); ok && custom != nil {
				legacy, perr := ParseDetail(custom.Detail)
				if perr == nil {
					return legacy, custom.Detail, nil
				}
			}
		}
		return LegacyDetail{}, "", fmt.Errorf("IconRecognition custom result is empty")
	}
	raw := ""
	if best := parsed.Best(); len(best) > 0 {
		if custom, ok := best[0].AsCustom(); ok && custom != nil {
			raw = custom.Detail
		}
	}
	return LegacyDetail{
		DetailVersion: 3,
		Matched:       true,
		Matches:       matches,
	}, raw, nil
}
