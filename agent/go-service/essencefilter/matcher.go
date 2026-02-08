package essencefilter

import (
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

var buildSlotIndicesOnce sync.Once

// MatchEssenceSkills - 先用原始清洗文本匹配，失败后再用相近字替换后的文本匹配
func MatchEssenceSkills(ctx *maa.Context, ocrSkills []string) (*SkillCombination, bool) {
	if len(ocrSkills) != 3 {
		log.Warn().Int("len", len(ocrSkills)).Strs("ocr_skills", ocrSkills).Msg("[EssenceFilter] MatchEssenceSkills: OCR 数量不足")
		return nil, false
	}

	buildSlotIndicesOnce.Do(buildSlotIndices)

	ocrSkillIDs := make([]int, 3)
	for i, skill := range ocrSkills {
		id, ok := matchSkillIDEnhanced(i+1, skill)
		if !ok {
			log.Info().Int("slot", i+1).Str("skill", skill).Msg("[EssenceFilter] MatchEssenceSkills: OCR 未匹配到技能 ID")
			return nil, false
		}
		ocrSkillIDs[i] = id
		log.Debug().Int("slot", i+1).Str("skill", skill).Int("skill_id", id).Msg("[EssenceFilter] OCR 技能映射结果")
	}

	for _, combination := range targetSkillCombinations {
		if len(combination.SkillIDs) == 3 &&
			ocrSkillIDs[0] == combination.SkillIDs[0] &&
			ocrSkillIDs[1] == combination.SkillIDs[1] &&
			ocrSkillIDs[2] == combination.SkillIDs[2] {
			log.Info().
				Str("weapon", combination.Weapon.ChineseName).
				Ints("ocr_skill_ids", ocrSkillIDs).
				Ints("expected_ids", combination.SkillIDs).
				Strs("ocr_skills", ocrSkills).
				Strs("expected_skills", combination.SkillsChinese).
				Msg("[EssenceFilter] MatchEssenceSkills: ID 匹配成功")
			return &combination, true
		}
	}

	log.Info().
		Ints("ocr_skill_ids", ocrSkillIDs).
		Strs("ocr_skills", ocrSkills).
		Int("target_combo_total", len(targetSkillCombinations)).
		Msg("[EssenceFilter] MatchEssenceSkills: 未找到匹配组合")

	return nil, false
}

// 预处理后的技能条目
type skillEntry struct {
	ID            int
	RawFull       string
	RawCore       string
	NormFull      string
	NormCore      string
	RawLen        int
	NormLen       int
	FirstCharRaw  string
	LastCharRaw   string
	FirstCharNorm string
	LastCharNorm  string
}

// 槽位索引：同时保存原始清洗串和相近字替换后的索引
type slotIndex struct {
	rawFullIndex  map[string][]int
	rawCoreIndex  map[string][]int
	normFullIndex map[string][]int
	normCoreIndex map[string][]int

	firstCharRaw  map[string][]int
	lastCharRaw   map[string][]int
	firstCharNorm map[string][]int
	lastCharNorm  map[string][]int

	entries []skillEntry
}

var slotIndices [3]slotIndex

// 构建技能索引（启动或首次匹配时）
func buildSlotIndices() {
	for i := 0; i < 3; i++ {
		pool := getPoolBySlot(i + 1)
		idx := slotIndex{
			rawFullIndex:  make(map[string][]int),
			rawCoreIndex:  make(map[string][]int),
			normFullIndex: make(map[string][]int),
			normCoreIndex: make(map[string][]int),
			firstCharRaw:  make(map[string][]int),
			lastCharRaw:   make(map[string][]int),
			firstCharNorm: make(map[string][]int),
			lastCharNorm:  make(map[string][]int),
		}
		for _, s := range pool {
			rawFull := cleanChinese(s.Chinese)
			rawCore := trimStopSuffix(rawFull)
			// 技能池不做相近字替换，保持原始文本，避免全局误替换
			normFull := rawFull
			normCore := rawCore

			e := skillEntry{
				ID:            s.ID,
				RawFull:       rawFull,
				RawCore:       rawCore,
				NormFull:      normFull,
				NormCore:      normCore,
				RawLen:        utf8.RuneCountInString(rawFull),
				NormLen:       utf8.RuneCountInString(normFull),
				FirstCharRaw:  firstChar(rawFull),
				LastCharRaw:   lastChar(rawFull),
				FirstCharNorm: firstChar(normFull),
				LastCharNorm:  lastChar(normFull),
			}

			if e.FirstCharRaw != "" {
				idx.firstCharRaw[e.FirstCharRaw] = append(idx.firstCharRaw[e.FirstCharRaw], s.ID)
			}
			if e.LastCharRaw != "" {
				idx.lastCharRaw[e.LastCharRaw] = append(idx.lastCharRaw[e.LastCharRaw], s.ID)
			}
			if e.FirstCharNorm != "" {
				idx.firstCharNorm[e.FirstCharNorm] = append(idx.firstCharNorm[e.FirstCharNorm], s.ID)
			}
			if e.LastCharNorm != "" {
				idx.lastCharNorm[e.LastCharNorm] = append(idx.lastCharNorm[e.LastCharNorm], s.ID)
			}

			idx.entries = append(idx.entries, e)
			idx.rawFullIndex[rawFull] = append(idx.rawFullIndex[rawFull], s.ID)
			idx.rawCoreIndex[rawCore] = append(idx.rawCoreIndex[rawCore], s.ID)
			idx.normFullIndex[normFull] = append(idx.normFullIndex[normFull], s.ID)
			idx.normCoreIndex[normCore] = append(idx.normCoreIndex[normCore], s.ID)
		}
		slotIndices[i] = idx
	}
}

// 清洗：只保留汉字
func cleanChinese(text string) string {
	var b strings.Builder
	for _, r := range text {
		if unicode.Is(unicode.Han, r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// trimStopSuffix - 去除停用后缀（从配置文件加载）
func trimStopSuffix(s string) string {
	for _, suf := range matcherConfig.SuffixStopwords {
		if strings.HasSuffix(s, suf) && utf8.RuneCountInString(s) > utf8.RuneCountInString(suf) {
			return strings.TrimSuffix(s, suf)
		}
	}
	return s
}

// normalizeSimilar - 相近/误识替换（键为误识，值为正确），仅作用于 OCR 文本，不改技能池（从配置文件加载）
func normalizeSimilar(s string) string {
	for old, val := range matcherConfig.SimilarWordMap {
		s = strings.ReplaceAll(s, old, val)
	}
	return s
}

// Damerau-Levenshtein，超过 max 早停
func editDistance(a, b string, max int) int {
	ra, rb := []rune(a), []rune(b)
	la, lb := len(ra), len(rb)
	if abs(la-lb) > max {
		return max + 1
	}
	dp := make([][]int, la+1)
	for i := range dp {
		dp[i] = make([]int, lb+1)
	}
	for i := 0; i <= la; i++ {
		dp[i][0] = i
	}
	for j := 0; j <= lb; j++ {
		dp[0][j] = j
	}
	for i := 1; i <= la; i++ {
		for j := 1; j <= lb; j++ {
			cost := 0
			if ra[i-1] != rb[j-1] {
				cost = 1
			}
			dp[i][j] = min3(
				dp[i-1][j]+1,
				dp[i][j-1]+1,
				dp[i-1][j-1]+cost,
			)
			if i > 1 && j > 1 && ra[i-1] == rb[j-2] && ra[i-2] == rb[j-1] {
				dp[i][j] = min(dp[i][j], dp[i-2][j-2]+cost)
			}
		}
	}
	if dp[la][lb] > max {
		return max + 1
	}
	return dp[la][lb]
}

// 先用原始，再用相近替换后的文本匹配；每阶段都有详细日志
func matchSkillIDEnhanced(slot int, ocrText string) (int, bool) {
	idx := slotIndices[slot-1]
	pool := getPoolBySlot(slot)
	idToName := make(map[int]string, len(pool))
	for _, s := range pool {
		idToName[s.ID] = s.Chinese
	}

	cleanedRaw := cleanChinese(ocrText)
	if cleanedRaw == "" {
		log.Debug().Int("slot", slot).Str("ocr_raw", ocrText).Msg("[EssenceFilter] match: cleaned empty")
		return 0, false
	}
	coreRaw := trimStopSuffix(cleanedRaw)

	if id, ok := attemptMatch("raw", slot, cleanedRaw, coreRaw, idx, idToName); ok {
		return id, true
	}

	cleanedNorm := normalizeSimilar(cleanedRaw)
	coreNorm := trimStopSuffix(cleanedNorm)
	// 若替换后无变化，仍再试一次，以保持日志区分
	if id, ok := attemptMatch("norm", slot, cleanedNorm, coreNorm, idx, idToName); ok {
		return id, true
	}

	log.Info().Int("slot", slot).Str("step", "no_match").Str("cleaned_raw", cleanedRaw).Str("cleaned_norm", cleanedNorm).Msg("[EssenceFilter] match miss")
	return 0, false
}

type matchPhase string

func attemptMatch(phase matchPhase, slot int, cleaned, core string, idx slotIndex, idToName map[int]string) (int, bool) {
	useNorm := phase == "norm"
	var fullIndex, coreIndex map[string][]int
	var firstChar, lastChar map[string][]int

	if useNorm {
		fullIndex, coreIndex = idx.normFullIndex, idx.normCoreIndex
		firstChar, lastChar = idx.firstCharNorm, idx.lastCharNorm
	} else {
		fullIndex, coreIndex = idx.rawFullIndex, idx.rawCoreIndex
		firstChar, lastChar = idx.firstCharRaw, idx.lastCharRaw
	}

	cLen := utf8.RuneCountInString(cleaned)
	coreLen := utf8.RuneCountInString(core)

	log.Debug().Int("slot", slot).Str("phase", string(phase)).Str("cleaned", cleaned).Str("core", core).Msg("[EssenceFilter] match: start")

	// 1) 完整精确
	if ids, ok := fullIndex[cleaned]; ok && len(ids) > 0 {
		log.Info().Int("slot", slot).Str("phase", string(phase)).Str("step", "exact_full").Str("cleaned", cleaned).
			Int("skill_id", ids[0]).Str("skill_name", idToName[ids[0]]).
			Msg("[EssenceFilter] match hit")
		return ids[0], true
	}
	// 2) 核心前缀精确
	if ids, ok := coreIndex[core]; ok && len(ids) > 0 {
		log.Info().Int("slot", slot).Str("phase", string(phase)).Str("step", "exact_core").Str("core", core).
			Int("skill_id", ids[0]).Str("skill_name", idToName[ids[0]]).
			Msg("[EssenceFilter] match hit")
		return ids[0], true
	}
	// 3) 完整子串（长度差 ≤2）
	for _, e := range idx.entries {
		tFull := e.RawFull
		tLen := e.RawLen
		if useNorm {
			tFull = e.NormFull
			tLen = e.NormLen
		}
		if abs(tLen-cLen) > 2 {
			continue
		}
		if strings.Contains(tFull, cleaned) {
			log.Info().Int("slot", slot).Str("phase", string(phase)).Str("step", "substring_full").
				Str("cleaned", cleaned).Str("target", tFull).
				Int("skill_id", e.ID).Str("skill_name", idToName[e.ID]).
				Msg("[EssenceFilter] match hit")
			return e.ID, true
		}
	}
	// 4) 核心子串（长度差 ≤2）
	for _, e := range idx.entries {
		tCore := e.RawCore
		tLen := e.RawLen
		if useNorm {
			tCore = e.NormCore
			tLen = e.NormLen
		}
		if abs(tLen-coreLen) > 2 {
			continue
		}
		if core != "" && strings.Contains(tCore, core) {
			log.Info().Int("slot", slot).Str("phase", string(phase)).Str("step", "substring_core").
				Str("core", core).Str("target_core", tCore).
				Int("skill_id", e.ID).Str("skill_name", idToName[e.ID]).
				Msg("[EssenceFilter] match hit")
			return e.ID, true
		}
	}
	// 5) 双字-单字兜底（首/尾且唯一）
	if cLen == 1 {
		if ids := firstChar[cleaned]; len(ids) == 1 {
			log.Info().Int("slot", slot).Str("phase", string(phase)).Str("step", "single_char_first").
				Str("char", cleaned).Int("skill_id", ids[0]).Str("skill_name", idToName[ids[0]]).
				Msg("[EssenceFilter] match hit")
			return ids[0], true
		}
		if ids := lastChar[cleaned]; len(ids) == 1 {
			log.Info().Int("slot", slot).Str("phase", string(phase)).Str("step", "single_char_last").
				Str("char", cleaned).Int("skill_id", ids[0]).Str("skill_name", idToName[ids[0]]).
				Msg("[EssenceFilter] match hit")
			return ids[0], true
		}
	}
	// 6) 编辑距兜底（保守：长度<4 允许 1，否则 2）
	maxEd := 1
	if cLen >= 4 {
		maxEd = 2
	}
	bestID, bestDist := 0, maxEd+1
	for _, e := range idx.entries {
		tFull := e.RawFull
		if useNorm {
			tFull = e.NormFull
		}
		dist := editDistance(cleaned, tFull, maxEd)
		if dist <= maxEd && dist < bestDist {
			bestID, bestDist = e.ID, dist
		}
	}
	if bestID != 0 {
		log.Info().Int("slot", slot).Str("phase", string(phase)).Str("step", "edit_distance").
			Str("cleaned", cleaned).Int("distance", bestDist).
			Int("skill_id", bestID).Str("skill_name", idToName[bestID]).
			Msg("[EssenceFilter] match hit")
		return bestID, true
	}
	return 0, false
}

// getPoolBySlot - 按槽位获取技能池
func getPoolBySlot(slot int) []SkillPool {
	switch slot {
	case 1:
		return weaponDB.SkillPools.Slot1
	case 2:
		return weaponDB.SkillPools.Slot2
	case 3:
		return weaponDB.SkillPools.Slot3
	default:
		return nil
	}
}

// skillNameByID - 按 ID 取技能中文名
func skillNameByID(id int, pool []SkillPool) string {
	for _, s := range pool {
		if s.ID == id {
			return s.Chinese
		}
	}
	return ""
}

func firstChar(s string) string {
	r := []rune(s)
	if len(r) == 2 {
		return string(r[0])
	}
	return ""
}

func lastChar(s string) string {
	r := []rune(s)
	if len(r) == 2 {
		return string(r[1])
	}
	return ""
}

// 小工具
func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func min3(a, b, c int) int {
	return min(a, min(b, c))
}
