package sellproduct

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

const sellProductSelectionDataResourcePath = "data/SellProduct/selection_data.json"

var (
	selectionDataOnce  sync.Once
	selectionDataCache *sellProductSelectionDataFile
	selectionDataErr   error
)

// sellProductSelectionDataFile 是生成器输出的最小运行时选品数据契约。
// 上游 zmdmap 结构、排序和临时过滤均在构建阶段消化，Go 只展开稳定引用。
type sellProductSelectionDataFile struct {
	Items         map[string]selectionDataItem     `json:"items"`
	Operators     map[string]selectionDataOperator `json:"operators"`
	LocationOrder []string                         `json:"location_order"`
	Locations     map[string]selectionDataLocation `json:"locations"`
}

type selectionDataItem struct {
	Names map[string]string `json:"names"`
}

type selectionDataOperator struct {
	CacheName string            `json:"cache_name"`
	Names     map[string]string `json:"names"`
}

type selectionDataTargetOperator struct {
	Name      string `json:"name"`
	BonusTier int    `json:"bonus_tier"`
}

type selectionDataLocation struct {
	Names            map[string]string             `json:"names"`
	ItemOrder        []string                      `json:"item_order"`
	TargetOperators  []selectionDataTargetOperator `json:"target_operators"`
	RestoreOperators []string                      `json:"restore_operators"`
}

func loadSellProductSelectionData() (*sellProductSelectionDataFile, error) {
	var data sellProductSelectionDataFile
	if err := readSellProductSelectionData(&data); err != nil {
		return nil, fmt.Errorf("read %s: %w", sellProductSelectionDataResourcePath, err)
	}
	if err := validateSellProductSelectionData(&data); err != nil {
		return nil, fmt.Errorf("validate %s: %w", sellProductSelectionDataResourcePath, err)
	}
	return &data, nil
}

// readSellProductSelectionData 在源码环境读取 assets 中的生成产物，发布环境读取 install/data。
func readSellProductSelectionData(out *sellProductSelectionDataFile) error {
	if sourcePath := sellProductSelectionDataSourcePath(); sourcePath != "" {
		if content, err := os.ReadFile(sourcePath); err == nil {
			return json.Unmarshal(content, out)
		}
	}
	return resource.ReadJsonResource(sellProductSelectionDataResourcePath, out)
}

func sellProductSelectionDataSourcePath() string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return ""
	}
	return filepath.Clean(filepath.Join(
		filepath.Dir(file),
		"..",
		"..",
		"..",
		"assets",
		filepath.FromSlash(sellProductSelectionDataResourcePath),
	))
}

func loadSellProductSelectionDataCached() (*sellProductSelectionDataFile, error) {
	selectionDataOnce.Do(func() {
		selectionDataCache, selectionDataErr = loadSellProductSelectionData()
	})
	return selectionDataCache, selectionDataErr
}

func validateSellProductSelectionData(data *sellProductSelectionDataFile) error {
	if data == nil {
		return fmt.Errorf("data is nil")
	}
	if len(data.Items) == 0 {
		return fmt.Errorf("item catalog is empty")
	}
	if len(data.Operators) == 0 {
		return fmt.Errorf("operator catalog is empty")
	}
	if len(data.LocationOrder) == 0 || len(data.Locations) == 0 {
		return fmt.Errorf("location catalog is empty")
	}
	return nil
}

func selectionItemPriorityGroup(data *sellProductSelectionDataFile, itemID string) (itemPriorityGroup, error) {
	itemID = strings.TrimSpace(itemID)
	item, ok := data.Items[itemID]
	if !ok {
		return itemPriorityGroup{}, fmt.Errorf("item %q not found", itemID)
	}
	candidates := selectionExpectedNames(item.Names)
	if len(candidates) == 0 {
		return itemPriorityGroup{}, fmt.Errorf("item %q expected names are empty", itemID)
	}
	return itemPriorityGroup{
		ItemID:      itemID,
		DisplayName: localizedSelectionName(item.Names, itemID),
		Candidates:  candidates,
	}, nil
}

func selectionOperatorCandidate(
	data *sellProductSelectionDataFile,
	name string,
	priority int,
	bonusTier int,
) (operatorCandidate, error) {
	name = strings.TrimSpace(name)
	operator, ok := data.Operators[name]
	if !ok {
		return operatorCandidate{}, fmt.Errorf("operator %q not found", name)
	}
	candidate := operatorCandidate{
		Name:        name,
		CacheName:   operator.CacheName,
		DisplayName: localizedSelectionName(operator.Names, name),
		Expected:    selectionExpectedNames(operator.Names),
		Priority:    priority,
		BonusTier:   bonusTier,
	}
	normalized := normalizeOperatorCandidates([]operatorCandidate{candidate})
	if len(normalized) == 0 {
		return operatorCandidate{}, fmt.Errorf("operator %q data is invalid", name)
	}
	return normalized[0], nil
}

func localizedSelectionName(names map[string]string, fallback string) string {
	lang := i18n.NormalizeLang(i18n.Lang())
	if name := strings.TrimSpace(names[lang]); name != "" {
		return name
	}
	if name := strings.TrimSpace(names[i18n.DefaultLang]); name != "" {
		return name
	}
	return strings.TrimSpace(fallback)
}

func selectionExpectedNames(names map[string]string) []string {
	return uniqueNonEmptyStrings([]string{
		names[i18n.LangZhCN],
		names[i18n.LangZhTW],
		names[i18n.LangEnUS],
		names[i18n.LangJaJP],
		names[i18n.LangKoKR],
	})
}
