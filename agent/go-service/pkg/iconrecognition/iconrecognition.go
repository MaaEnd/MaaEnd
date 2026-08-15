// Package iconrecognition 提供 IconRecognition 的 Go 协议数据类型与解析辅助。
package iconrecognition

import (
    "encoding/json"
    "fmt"
    "slices"
    "strings"

    maa "github.com/MaaXYZ/maa-framework-go/v4"
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

// ItemFilter 是 storageKind:categoryType 形式的候选过滤器。
type ItemFilter string

// NormalFilters 提供 Normal 存储下的合法分类过滤器。
type NormalFilters struct {
    Any            ItemFilter
    Ore            ItemFilter
    Plant          ItemFilter
    Product        ItemFilter
    Doodad         ItemFilter
    Nurturance     ItemFilter
    Usable         ItemFilter
    Producer       ItemFilter
    PortableDevice ItemFilter
}

// ValuableDepotFilters 提供 ValuableDepot 存储下的合法分类过滤器。
type ValuableDepotFilters struct {
    Any            ItemFilter
    CommercialItem ItemFilter
    SpecialItem    ItemFilter
    Weapon         ItemFilter
}

// IsolateFilters 提供 Isolate 存储下的合法分类过滤器。
type IsolateFilters struct {
    Any           ItemFilter
    AdventureExp  ItemFilter
    BPExp         ItemFilter
    Diamond       ItemFilter
    DomainGold    ItemFilter
    Gold          ItemFilter
    Originium     ItemFilter
    SpaceshipGold ItemFilter
    WeaponGold    ItemFilter
}

// StorageFilters 是 IconRecognition 合法 storageKind:categoryType 过滤器的命名空间。
type StorageFilters struct {
    Normal        NormalFilters
    ValuableDepot ValuableDepotFilters
    Isolate       IsolateFilters
}

// StorageFilter 创建不允许非法 storage/category 组合的字面量式过滤器访问。
func StorageFilter() StorageFilters {
    return StorageFilters{
        Normal: NormalFilters{
            Any:            ItemFilter("Normal:*"),
            Ore:            ItemFilter("Normal:Ore"),
            Plant:          ItemFilter("Normal:Plant"),
            Product:        ItemFilter("Normal:Product"),
            Doodad:         ItemFilter("Normal:Doodad"),
            Nurturance:     ItemFilter("Normal:Nurturance"),
            Usable:         ItemFilter("Normal:Usable"),
            Producer:       ItemFilter("Normal:Producer"),
            PortableDevice: ItemFilter("Normal:PortableDevice"),
        },
        ValuableDepot: ValuableDepotFilters{
            Any:            ItemFilter("ValuableDepot:*"),
            CommercialItem: ItemFilter("ValuableDepot:CommercialItem"),
            SpecialItem:    ItemFilter("ValuableDepot:SpecialItem"),
            Weapon:         ItemFilter("ValuableDepot:Weapon"),
        },
        Isolate: IsolateFilters{
            Any:           ItemFilter("Isolate:*"),
            AdventureExp:  ItemFilter("Isolate:AdventureExp"),
            BPExp:         ItemFilter("Isolate:BPExp"),
            Diamond:       ItemFilter("Isolate:Diamond"),
            DomainGold:    ItemFilter("Isolate:DomainGold"),
            Gold:          ItemFilter("Isolate:Gold"),
            Originium:     ItemFilter("Isolate:Originium"),
            SpaceshipGold: ItemFilter("Isolate:SpaceshipGold"),
            WeaponGold:    ItemFilter("Isolate:WeaponGold"),
        },
    }
}

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
    DetailVersion int      `json:"detail_version"`
    Matched       bool     `json:"matched"`
    GridType      GridType `json:"grid_type"`
    ROI           maa.Rect `json:"roi"`
    Matches       []Match  `json:"matches"`
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
