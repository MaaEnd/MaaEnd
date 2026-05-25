package creditshopping

import (
	"encoding/json"
	"unicode"
)

// UnmarshalJSON 兼容 schema v1 的 item_id 字段，避免读旧快照后 upsert 回写时丢失 id。
func (s *SlotRecord) UnmarshalJSON(data []byte) error {
	type slotRecordV2 SlotRecord
	var legacy struct {
		slotRecordV2
		ItemID string `json:"item_id"`
	}
	if err := json.Unmarshal(data, &legacy); err != nil {
		return err
	}
	*s = SlotRecord(legacy.slotRecordV2)
	if s.ID == "" && legacy.ItemID != "" {
		s.ID = legacy.ItemID
	}
	return nil
}

// migrateSlotRecord 将 v1 槽位补全为 v2：仅有 item_id（已映射到 ID）时，用 item_map 反查展示名填入 name。
func migrateSlotRecord(slot *SlotRecord) bool {
	if slot == nil || slot.Name != "" || slot.ID == "" {
		return false
	}
	slot.Name = displayNameForCreditItemID(slot.ID)
	return slot.Name != ""
}

// displayNameForCreditItemID 从 item_map 中为内部 ID 选取一个展示用别名（优先含 CJK 的条目）。
func displayNameForCreditItemID(id string) string {
	if id == "" {
		return ""
	}
	m, err := getCreditItemMap()
	if err != nil || m == nil || len(m.aliasToID) == 0 {
		return id
	}
	best := ""
	bestScore := -1
	for alias, itemID := range m.aliasToID {
		if itemID != id {
			continue
		}
		score := aliasDisplayPreferenceScore(alias)
		if score > bestScore || score == bestScore && (best == "" || alias < best) {
			bestScore = score
			best = alias
		}
	}
	if best != "" {
		return best
	}
	return id
}

func aliasDisplayPreferenceScore(alias string) int {
	score := 0
	for _, r := range alias {
		if unicode.Is(unicode.Han, r) {
			score += 4
			continue
		}
		if r > 127 {
			score += 3
			continue
		}
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			score++
		}
	}
	return score
}
