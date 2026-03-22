package essencefilter

import (
	"fmt"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/essencefilter/matchapi"
)

func efMsgOCRSkills(locale string, skills []string, levels [3]int) string {
	loc := matchapi.NormalizeInputLocale(locale)
	switch loc {
	case matchapi.LocaleEN:
		return fmt.Sprintf("OCR skills: %s(+%d) | %s(+%d) | %s(+%d)", skills[0], levels[0], skills[1], levels[1], skills[2], levels[2])
	case matchapi.LocaleJP:
		return fmt.Sprintf("OCRスキル: %s(+%d) | %s(+%d) | %s(+%d)", skills[0], levels[0], skills[1], levels[1], skills[2], levels[2])
	case matchapi.LocaleKR:
		return fmt.Sprintf("OCR 스킬: %s(+%d) | %s(+%d) | %s(+%d)", skills[0], levels[0], skills[1], levels[1], skills[2], levels[2])
	case matchapi.LocaleTC:
		return fmt.Sprintf("OCR到技能：%s(+%d) | %s(+%d) | %s(+%d)", skills[0], levels[0], skills[1], levels[1], skills[2], levels[2])
	default:
		return fmt.Sprintf("OCR到技能：%s(+%d) | %s(+%d) | %s(+%d)", skills[0], levels[0], skills[1], levels[1], skills[2], levels[2])
	}
}

func efMsgMatchedWeapons(locale string, weaponsHTML string) string {
	loc := matchapi.NormalizeInputLocale(locale)
	switch loc {
	case matchapi.LocaleEN:
		return fmt.Sprintf(`<div style="color: #064d7c; font-weight: 900;">Matched weapons: %s</div>`, weaponsHTML)
	case matchapi.LocaleJP:
		return fmt.Sprintf(`<div style="color: #064d7c; font-weight: 900;">一致武器: %s</div>`, weaponsHTML)
	case matchapi.LocaleKR:
		return fmt.Sprintf(`<div style="color: #064d7c; font-weight: 900;">일치 무기: %s</div>`, weaponsHTML)
	case matchapi.LocaleTC:
		return fmt.Sprintf(`<div style="color: #064d7c; font-weight: 900;">匹配到武器：%s</div>`, weaponsHTML)
	default:
		return fmt.Sprintf(`<div style="color: #064d7c; font-weight: 900;">匹配到武器：%s</div>`, weaponsHTML)
	}
}

func efMsgExtRuleLock(locale string, reason string) string {
	loc := matchapi.NormalizeInputLocale(locale)
	switch loc {
	case matchapi.LocaleEN:
		return fmt.Sprintf(`<div style="color: #064d7c; font-weight: 900;">🔒 Extension rule hit and locked: %s</div>`, escapeHTML(reason))
	case matchapi.LocaleJP:
		return fmt.Sprintf(`<div style="color: #064d7c; font-weight: 900;">🔒 拡張ルール一致でロック: %s</div>`, escapeHTML(reason))
	case matchapi.LocaleKR:
		return fmt.Sprintf(`<div style="color: #064d7c; font-weight: 900;">🔒 확장 규칙 적중 및 잠금: %s</div>`, escapeHTML(reason))
	case matchapi.LocaleTC:
		return fmt.Sprintf(`<div style="color: #064d7c; font-weight: 900;">🔒 擴展規則命中並鎖定：%s</div>`, escapeHTML(reason))
	default:
		return fmt.Sprintf(`<div style="color: #064d7c; font-weight: 900;">🔒 扩展规则命中并锁定：%s</div>`, escapeHTML(reason))
	}
}

func efMsgExtRuleNoop(locale string, reason string) string {
	loc := matchapi.NormalizeInputLocale(locale)
	switch loc {
	case matchapi.LocaleEN:
		return fmt.Sprintf(`<div style="color: #d18b00; font-weight: 900;">🗂️ Extension rule hit (no action): %s</div>`, escapeHTML(reason))
	case matchapi.LocaleJP:
		return fmt.Sprintf(`<div style="color: #d18b00; font-weight: 900;">🗂️ 拡張ルール一致（操作なし）: %s</div>`, escapeHTML(reason))
	case matchapi.LocaleKR:
		return fmt.Sprintf(`<div style="color: #d18b00; font-weight: 900;">🗂️ 확장 규칙 적중 (동작 없음): %s</div>`, escapeHTML(reason))
	case matchapi.LocaleTC:
		return fmt.Sprintf(`<div style="color: #d18b00; font-weight: 900;">🗂️ 擴展規則命中（不操作）：%s</div>`, escapeHTML(reason))
	default:
		return fmt.Sprintf(`<div style="color: #d18b00; font-weight: 900;">🗂️ 扩展规则命中（不操作）：%s</div>`, escapeHTML(reason))
	}
}

func efMsgNoMatchDiscard(locale string) string {
	loc := matchapi.NormalizeInputLocale(locale)
	switch loc {
	case matchapi.LocaleEN:
		return `<div style="color: #ff6b6b; font-weight: 900;">🗑️ No target skill combination matched, discarded</div>`
	case matchapi.LocaleJP:
		return `<div style="color: #ff6b6b; font-weight: 900;">🗑️ 目標スキル組み合わせに一致せず、破棄</div>`
	case matchapi.LocaleKR:
		return `<div style="color: #ff6b6b; font-weight: 900;">🗑️ 목표 스킬 조합 불일치, 폐기</div>`
	case matchapi.LocaleTC:
		return `<div style="color: #ff6b6b; font-weight: 900;">🗑️ 未匹配到目標技能組合，廢棄該物品</div>`
	default:
		return `<div style="color: #ff6b6b; font-weight: 900;">🗑️ 未匹配到目标技能组合，废弃该物品</div>`
	}
}

func efMsgNoMatchSkip(locale string) string {
	loc := matchapi.NormalizeInputLocale(locale)
	switch loc {
	case matchapi.LocaleEN:
		return "No target skill combination matched, skip this item"
	case matchapi.LocaleJP:
		return "目標スキル組み合わせに一致せず、このアイテムをスキップ"
	case matchapi.LocaleKR:
		return "목표 스킬 조합 불일치, 이 아이템 건너뜀"
	case matchapi.LocaleTC:
		return "未匹配到目標技能組合，跳過該物品"
	default:
		return "未匹配到目标技能组合，跳过该物品"
	}
}

