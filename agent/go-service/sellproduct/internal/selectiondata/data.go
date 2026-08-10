// Package selectiondata 加载货品与干员共享的 SellProduct 运行时生成数据。
package selectiondata

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/resource"
)

// ResourcePath 是资源目录内 SellProduct 运行时生成数据的路径。
const ResourcePath = "data/SellProduct/selection_data.json"

var (
	selectionDataOnce  sync.Once
	selectionDataCache *File
	selectionDataErr   error
)

// File 是生成器输出的最小运行时选品数据契约。
// 上游精简数据已消化原始 TableCfg 结构，临时过滤由 MaaEnd 构建阶段处理。
// 具体选品顺序由 Go 策略决定。
type File struct {
	Items         map[string]Item     `json:"items"`
	Operators     map[string]Operator `json:"operators"`
	LocationOrder []string            `json:"location_order"`
	Locations     map[string]Location `json:"locations"`
}

// Item 描述一个可选货品。
type Item struct {
	Names map[string]string `json:"names"`
}

// LocationItem 描述某据点的一项可售货品及其价值属性。
type LocationItem struct {
	ItemID    string `json:"item_id"`
	Rarity    int    `json:"rarity"`
	UnitPrice int    `json:"unit_price"`
}

// Operator 描述一名干员及其本地化名称。
type Operator struct {
	Names map[string]string `json:"names"`
}

// TargetOperator 描述一名干员候选及其售卖加成档位。
type TargetOperator struct {
	Name                          string `json:"name"`
	BonusTier                     int    `json:"bonus_tier"`
	OutpostProsperityMaxBonusTier int    `json:"outpost_prosperity_max_bonus_tier"`
}

// Location 描述某据点生成的货品与干员顺序。
type Location struct {
	Names            map[string]string `json:"names"`
	Items            []LocationItem    `json:"items"`
	TargetOperators  []TargetOperator  `json:"target_operators"`
	RestoreOperators []string          `json:"restore_operators"`
}

// Load 读取并校验生成的 SellProduct 运行时数据。
func Load() (*File, error) {
	var data File
	if err := read(&data); err != nil {
		return nil, fmt.Errorf("read %s: %w", ResourcePath, err)
	}
	if err := Validate(&data); err != nil {
		return nil, fmt.Errorf("validate %s: %w", ResourcePath, err)
	}
	return &data, nil
}

// read 在源码环境读取 assets 中的生成产物，发布环境读取 install/data。
func read(out *File) error {
	if sourcePath := sourcePath(); sourcePath != "" {
		if content, err := os.ReadFile(sourcePath); err == nil {
			return json.Unmarshal(content, out)
		}
	}
	return resource.ReadJsonResource(ResourcePath, out)
}

func sourcePath() string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return ""
	}
	return filepath.Clean(filepath.Join(
		filepath.Dir(file),
		"..",
		"..",
		"..",
		"..",
		"..",
		"assets",
		filepath.FromSlash(ResourcePath),
	))
}

// LoadCached 在 Agent 进程生命周期内复用不可变的运行时数据。
func LoadCached() (*File, error) {
	selectionDataOnce.Do(func() {
		selectionDataCache, selectionDataErr = Load()
	})
	return selectionDataCache, selectionDataErr
}

// Validate 校验货品与干员两领域共同依赖的最小目录不变量。
func Validate(data *File) error {
	if data == nil {
		return fmt.Errorf("data is nil")
	}
	if len(data.LocationOrder) == 0 || len(data.Locations) == 0 {
		return fmt.Errorf("location catalog is empty")
	}
	return nil
}

// ValidateGoods 校验货品选择依赖的目录不变量。
func ValidateGoods(data *File) error {
	if err := Validate(data); err != nil {
		return err
	}
	if len(data.Items) == 0 {
		return fmt.Errorf("item catalog is empty")
	}
	return nil
}

// ValidateOperators 校验干员选择依赖的目录不变量。
func ValidateOperators(data *File) error {
	if err := Validate(data); err != nil {
		return err
	}
	if len(data.Operators) == 0 {
		return fmt.Errorf("operator catalog is empty")
	}
	return nil
}

// LocalizedName 返回当前语言名称；缺失时回退到默认语言。
func LocalizedName(names map[string]string, fallback string) string {
	lang := i18n.NormalizeLang(i18n.Lang())
	if name := strings.TrimSpace(names[lang]); name != "" {
		return name
	}
	if name := strings.TrimSpace(names[i18n.DefaultLang]); name != "" {
		return name
	}
	return strings.TrimSpace(fallback)
}

// LocationName 返回当前语言的据点名称；数据不可用或据点不存在时回退到稳定 ID。
func LocationName(location string) string {
	data, err := LoadCached()
	if err == nil {
		if entry, ok := data.Locations[location]; ok {
			return LocalizedName(entry.Names, location)
		}
	}
	return strings.TrimSpace(location)
}

// ItemName 返回当前语言的货品名称；数据不可用或货品不存在时回退到稳定 ID。
func ItemName(itemID string) string {
	data, err := LoadCached()
	if err == nil {
		if entry, ok := data.Items[itemID]; ok {
			return LocalizedName(entry.Names, itemID)
		}
	}
	return strings.TrimSpace(itemID)
}

// OperatorName 返回当前语言的干员名称；数据不可用或干员不存在时回退到稳定 ID。
func OperatorName(operator string) string {
	data, err := LoadCached()
	if err == nil {
		if entry, ok := data.Operators[operator]; ok {
			return LocalizedName(entry.Names, operator)
		}
	}
	return strings.TrimSpace(operator)
}

// ExpectedNames 按仓库语言顺序返回去重后的 OCR 候选名称。
func ExpectedNames(names map[string]string) []string {
	return uniqueNonEmptyStrings([]string{
		names[i18n.LangZhCN],
		names[i18n.LangZhTW],
		names[i18n.LangEnUS],
		names[i18n.LangJaJP],
		names[i18n.LangKoKR],
	})
}

func uniqueNonEmptyStrings(values []string) []string {
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}
