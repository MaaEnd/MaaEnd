package autostockpile

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/autostockpile/levenshtein"
)

//go:embed item_map.json
var itemMapJSON []byte

// ItemMap 存储商品名称与 ID 的双向映射。
type ItemMap struct {
	NameToID map[string]string
	IDToName map[string]string
}

var (
	cachedItemMap   *ItemMap
	cachedItemMapMu sync.RWMutex
)

// LoadItemMap 从嵌入的 item_map.json 加载商品映射数据。
// 参数 locale 指定语言区域（如 "zh_cn"）。
func LoadItemMap(locale string) (*ItemMap, error) {
	var raw map[string]map[string]string
	if err := json.Unmarshal(itemMapJSON, &raw); err != nil {
		return nil, fmt.Errorf("failed to parse embedded item_map.json: %w", err)
	}

	localeData, ok := raw[locale]
	if !ok {
		return nil, fmt.Errorf("locale %s not found in item_map.json", locale)
	}

	nameToID := make(map[string]string, len(localeData))
	idToName := make(map[string]string, len(localeData))

	for name, id := range localeData {
		nameToID[name] = id
		idToName[id] = name
	}

	return &ItemMap{
		NameToID: nameToID,
		IDToName: idToName,
	}, nil
}

// InitItemMap 初始化并缓存 ItemMap，仅在首次成功调用时加载文件。
func InitItemMap(locale string) error {
	cachedItemMapMu.RLock()
	if cachedItemMap != nil {
		cachedItemMapMu.RUnlock()
		return nil
	}
	cachedItemMapMu.RUnlock()

	cachedItemMapMu.Lock()
	defer cachedItemMapMu.Unlock()

	if cachedItemMap != nil {
		return nil
	}

	itemMap, err := LoadItemMap(locale)
	if err != nil {
		return err
	}
	cachedItemMap = itemMap
	return nil
}

// GetItemMap 返回缓存的 ItemMap，若未初始化则返回空映射。
func GetItemMap() *ItemMap {
	cachedItemMapMu.RLock()
	defer cachedItemMapMu.RUnlock()

	if cachedItemMap == nil {
		return &ItemMap{
			NameToID: make(map[string]string),
			IDToName: make(map[string]string),
		}
	}
	return cachedItemMap
}

// MatchGoodsName 使用 Levenshtein 距离匹配 OCR 文本到商品名称。
// 返回匹配的商品 ID、规范名称以及是否成功匹配。
// 仅返回编辑距离 ≤ maxDistance 的最佳匹配。
func MatchGoodsName(ocrText string, itemMap *ItemMap, maxDistance int) (id string, name string, matched bool) {
	if itemMap == nil || len(itemMap.NameToID) == 0 {
		return "", "", false
	}

	bestDistance := maxDistance + 1
	bestName := ""
	bestID := ""

	for candidateName, candidateID := range itemMap.NameToID {
		dist := levenshtein.Distance(ocrText, candidateName)
		if dist <= maxDistance && dist < bestDistance {
			bestDistance = dist
			bestName = candidateName
			bestID = candidateID
		}
	}

	if bestDistance <= maxDistance {
		return bestID, bestName, true
	}
	return "", "", false
}

// ParseTierFromID 从商品 ID 中提取 Tier 信息。
// 例如：输入 "ValleyIV/OriginiumSaplings.Tier3" 返回 "ValleyIVTier3"
func ParseTierFromID(id string) string {
	parts := strings.Split(id, "/")
	if len(parts) < 2 {
		return ""
	}

	region := parts[0]
	itemPart := parts[1]

	dotParts := strings.Split(itemPart, ".")
	if len(dotParts) < 2 {
		return ""
	}

	tierPart := dotParts[len(dotParts)-1]
	if !strings.HasPrefix(tierPart, "Tier") {
		return ""
	}

	return region + tierPart
}

// BuildTemplatePath 根据商品 ID 构造模板路径。
// 例如：输入 "ValleyIV/OriginiumSaplings.Tier3" 返回 "AutoStockpile/Goods/ValleyIV/OriginiumSaplings.Tier3.png"
func BuildTemplatePath(id string) string {
	return filepath.ToSlash(filepath.Join("AutoStockpile", "Goods", id+".png"))
}
