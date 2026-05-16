package creditshopping

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/levenshtein"
	"github.com/rs/zerolog/log"
)

//go:embed item_map.json
var creditItemMapJSON []byte

const creditItemMatchMaxDistance = 2

type creditItemMap struct {
	aliasToID map[string]string
}

var (
	cachedCreditItemMap   *creditItemMap
	cachedCreditItemMapMu sync.RWMutex
)

func getCreditItemMap() (*creditItemMap, error) {
	cachedCreditItemMapMu.RLock()
	if cachedCreditItemMap != nil {
		m := cachedCreditItemMap
		cachedCreditItemMapMu.RUnlock()
		return m, nil
	}
	cachedCreditItemMapMu.RUnlock()

	cachedCreditItemMapMu.Lock()
	defer cachedCreditItemMapMu.Unlock()
	if cachedCreditItemMap != nil {
		return cachedCreditItemMap, nil
	}
	m, err := loadCreditItemMap()
	if err != nil {
		return nil, err
	}
	cachedCreditItemMap = m
	return m, nil
}

func loadCreditItemMap() (*creditItemMap, error) {
	var raw map[string]string
	if err := json.Unmarshal(creditItemMapJSON, &raw); err != nil {
		return nil, fmt.Errorf("parse credit shopping item_map.json: %w", err)
	}
	return &creditItemMap{aliasToID: raw}, nil
}

// matchCreditItemID 将 OCR 名称匹配为 CreditShoppingItems case 名（如 Protoprism）。
func matchCreditItemID(ocrText string) (id string, matched bool) {
	m, err := getCreditItemMap()
	if err != nil {
		log.Warn().Err(err).Str("component", component).Msg("credit item map unavailable")
		return "", false
	}
	if m == nil || len(m.aliasToID) == 0 {
		return "", false
	}
	if id, ok := m.aliasToID[ocrText]; ok {
		return id, true
	}
	bestDistance := creditItemMatchMaxDistance + 1
	bestID := ""
	bestAlias := ""
	for alias, itemID := range m.aliasToID {
		dist := levenshtein.Distance(ocrText, alias)
		if dist <= creditItemMatchMaxDistance && (dist < bestDistance || dist == bestDistance && alias < bestAlias) {
			bestDistance = dist
			bestID = itemID
			bestAlias = alias
		}
	}
	if bestDistance <= creditItemMatchMaxDistance {
		return bestID, true
	}
	return "", false
}
