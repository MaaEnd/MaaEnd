package iconrecognition

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

// GridType 是 IconRecognition 支持的网格布局类型。
type GridType string

const (
	GridTypeTrade        GridType = "trade"
	GridTypeTransfer     GridType = "transfer"
	GridTypePortStorager GridType = "port_storager"
	GridTypeValuables    GridType = "valuables"
	GridTypeShipment     GridType = "shipment"
	GridTypeCreditTrade  GridType = "credit_trade"
	GridTypeRewards      GridType = "rewards"
	GridTypeSingleROI    GridType = "single_roi"
)

// CustomRecognitionName 是 Maa Pipeline 注册的 IconRecognition 名称。
const CustomRecognitionName = "IconRecognition"

// Params 是 IconRecognition custom_recognition_param 的公共表示。
// 候选 ID、基础/附加/排除条件和反查过滤器是否必填由具体调用场景决定。
type Params struct {
	GridType                   GridType     `json:"grid_type"`
	ItemIDs                    []string     `json:"item_ids,omitempty"`
	ItemFilters                []ItemFilter `json:"item_filters,omitempty"`
	AdditionalItemFilters      []ItemFilter `json:"additional_item_filters,omitempty"`
	ExcludedItemIDs            []string     `json:"excluded_item_ids,omitempty"`
	ItemRecheckFilters         []ItemFilter `json:"item_recheck_filters,omitempty"`
	RecognizeRegionUnavailable *bool        `json:"recognize_region_unavailable,omitempty"`
	Threshold                  *float64     `json:"threshold,omitempty"`
	SubpixelThreshold          *float64     `json:"subpixel_threshold,omitempty"`
	Deduplicate                *bool        `json:"deduplicate,omitempty"`
	Debug                      *bool        `json:"debug,omitempty"`
}

// Option 配置 NewParams 创建的 IconRecognition 参数。
type Option func(*Params)

// NewParams 使用 functional options 创建 IconRecognition 参数。
func NewParams(options ...Option) Params {
	params := Params{}
	for _, option := range options {
		if option != nil {
			option(&params)
		}
	}
	return params
}

// WithGridType 配置网格类型，并清理两端的空白。
func WithGridType(gridType GridType) Option {
	return func(params *Params) {
		params.GridType = GridType(strings.TrimSpace(string(gridType)))
	}
}

// WithItemIDs 配置候选物品 ID，清理空白并稳定去重。
func WithItemIDs(itemIDs ...string) Option {
	values := normalizeStrings(itemIDs)
	return func(params *Params) {
		params.ItemIDs = slices.Clone(values)
	}
}

// WithItemFilters 配置候选过滤器，清理空白并稳定去重；优先使用 StorageFilter 提供的已知组合。
func WithItemFilters(itemFilters ...ItemFilter) Option {
	values := normalizeItemFilters(itemFilters)
	return func(params *Params) {
		params.ItemFilters = slices.Clone(values)
	}
}

// WithAdditionalItemFilters 配置与基础候选取交集后追加的过滤器，并稳定去重。
func WithAdditionalItemFilters(itemFilters ...ItemFilter) Option {
	values := normalizeItemFilters(itemFilters)
	return func(params *Params) {
		params.AdditionalItemFilters = slices.Clone(values)
	}
}

// WithExcludedItemIDs 配置候选集合完成追加后需要排除的物品 ID，并稳定去重。
func WithExcludedItemIDs(itemIDs ...string) Option {
	values := normalizeStrings(itemIDs)
	return func(params *Params) {
		params.ExcludedItemIDs = slices.Clone(values)
	}
}

// WithItemRecheckFilters 配置 item_ids 候选的单格反查过滤器，并稳定去重。
func WithItemRecheckFilters(itemFilters ...ItemFilter) Option {
	values := normalizeItemFilters(itemFilters)
	return func(params *Params) {
		params.ItemRecheckFilters = slices.Clone(values)
	}
}

// WithRecognizeRegionUnavailable 配置是否在普通识别失败后尝试识别当前地区不可用的物品。
func WithRecognizeRegionUnavailable(enabled bool) Option {
	return func(params *Params) {
		params.RecognizeRegionUnavailable = pointerOf(enabled)
	}
}

// WithThreshold 配置最终匹配阈值；显式传入 0 时仍会序列化。
func WithThreshold(threshold float64) Option {
	return func(params *Params) {
		params.Threshold = pointerOf(threshold)
	}
}

// WithSubpixelThreshold 配置启动亚像素细化的最低分数；显式传入 0 时仍会序列化。
func WithSubpixelThreshold(threshold float64) Option {
	return func(params *Params) {
		params.SubpixelThreshold = pointerOf(threshold)
	}
}

// WithDeduplicate 配置是否按 item_id 去重；显式传入 false 时仍会序列化。
func WithDeduplicate(deduplicate bool) Option {
	return func(params *Params) {
		params.Deduplicate = pointerOf(deduplicate)
	}
}

// WithDebug 配置是否生成调试诊断；显式传入 false 时仍会序列化。
func WithDebug(debug bool) Option {
	return func(params *Params) {
		params.Debug = pointerOf(debug)
	}
}

// WithTuningFrom 复制另一组参数的阈值、去重和调试选项，不复制候选条件或场景行为。
func WithTuningFrom(source Params) Option {
	return func(params *Params) {
		params.Threshold = clonePointer(source.Threshold)
		params.SubpixelThreshold = clonePointer(source.SubpixelThreshold)
		params.Deduplicate = clonePointer(source.Deduplicate)
		params.Debug = clonePointer(source.Debug)
	}
}

// ParseParams 解析 custom_recognition_param JSON。
func ParseParams(raw string) (Params, error) {
	var params Params
	if strings.TrimSpace(raw) == "" {
		return params, fmt.Errorf("IconRecognition params are empty")
	}
	if err := json.Unmarshal([]byte(raw), &params); err != nil {
		return params, fmt.Errorf("parse IconRecognition params: %w", err)
	}
	params.GridType = GridType(strings.TrimSpace(string(params.GridType)))
	params.ItemIDs = normalizeStrings(params.ItemIDs)
	params.ItemFilters = normalizeItemFilters(params.ItemFilters)
	params.AdditionalItemFilters = normalizeItemFilters(params.AdditionalItemFilters)
	params.ExcludedItemIDs = normalizeStrings(params.ExcludedItemIDs)
	params.ItemRecheckFilters = normalizeItemFilters(params.ItemRecheckFilters)
	if params.GridType == "" {
		return params, fmt.Errorf("IconRecognition grid_type is required")
	}
	return params, nil
}

func normalizeStrings(values []string) []string {
	return normalizeUnique(values, strings.TrimSpace)
}

func normalizeItemFilters(values []ItemFilter) []ItemFilter {
	return normalizeUnique(values, func(value ItemFilter) ItemFilter {
		return ItemFilter(strings.TrimSpace(string(value)))
	})
}

func normalizeUnique[T comparable](values []T, normalize func(T) T) []T {
	result := make([]T, 0, len(values))
	seen := make(map[T]struct{}, len(values))
	for _, value := range values {
		normalized := normalize(value)
		if _, exists := seen[normalized]; exists {
			continue
		}
		seen[normalized] = struct{}{}
		result = append(result, normalized)
	}
	return result
}

func clonePointer[T any](value *T) *T {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func pointerOf[T any](value T) *T {
	return &value
}
