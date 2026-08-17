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
// ItemIDs 和 ItemFilters 是否必填由具体调用场景决定。
type Params struct {
	GridType          GridType     `json:"grid_type"`
	ItemIDs           []string     `json:"item_ids,omitempty"`
	ItemFilters       []ItemFilter `json:"item_filters,omitempty"`
	Threshold         *float64     `json:"threshold,omitempty"`
	SubpixelThreshold *float64     `json:"subpixel_threshold,omitempty"`
	Deduplicate       *bool        `json:"deduplicate,omitempty"`
	Debug             *bool        `json:"debug,omitempty"`
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

// WithItemIDs 配置候选物品 ID，并清理每个值两端的空白。
func WithItemIDs(itemIDs ...string) Option {
	values := normalizeStrings(itemIDs)
	return func(params *Params) {
		params.ItemIDs = slices.Clone(values)
	}
}

// WithItemFilters 配置候选过滤器；优先使用 StorageFilter 提供的已知组合。
func WithItemFilters(itemFilters ...ItemFilter) Option {
	values := normalizeItemFilters(itemFilters)
	return func(params *Params) {
		params.ItemFilters = slices.Clone(values)
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

// WithTuningFrom 复制另一组参数的阈值、去重和调试选项，不复制候选条件。
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
	if params.GridType == "" {
		return params, fmt.Errorf("IconRecognition grid_type is required")
	}
	return params, nil
}

func normalizeStrings(values []string) []string {
	result := make([]string, len(values))
	for index, value := range values {
		result[index] = strings.TrimSpace(value)
	}
	return result
}

func normalizeItemFilters(values []ItemFilter) []ItemFilter {
	result := make([]ItemFilter, len(values))
	for index, value := range values {
		result[index] = ItemFilter(strings.TrimSpace(string(value)))
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
