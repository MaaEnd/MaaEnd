package essencefilter

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

var levelParseRe = regexp.MustCompile(`\+?(\d+)`)

// --- Init ---

// EssenceFilterInitAction - initialize filter
type EssenceFilterInitAction struct{}

func (a *EssenceFilterInitAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	log.Info().Str("component", "EssenceFilter").Msg("init start")
	base := getResourceBase()
	if base == "" {
		base = "data"
	}
	gameDataDir := filepath.Join(base, "EssenceFilter")
	matcherConfigPath := filepath.Join(gameDataDir, "matcher_config.json")
	if err := LoadMatcherConfig(matcherConfigPath); err != nil {
		log.Error().Err(err).Str("component", "EssenceFilter").Str("step", "LoadMatcherConfig").Msg("load matcher config failed")
		return false
	}
	log.Info().Str("component", "EssenceFilter").Str("step", "LoadMatcherConfig").Msg("matcher config loaded")
	if err := LoadNewFormat(gameDataDir); err != nil {
		log.Error().Err(err).Str("component", "EssenceFilter").Str("step", "LoadDatabase").Msg("load DB failed")
		return false
	}
	LogMXUSimpleHTML(ctx, "武器数据加载完成")
	logSkillPools()

	opts, err := getOptionsFromAttach(ctx, arg.CurrentTaskName)
	if err != nil {
		log.Error().Err(err).Str("component", "EssenceFilter").Str("step", "LoadOptions").Msg("load options failed")
		return false
	}
	var WeaponRarity []int
	if opts.Rarity6Weapon {
		WeaponRarity = append(WeaponRarity, 6)
	}
	if opts.Rarity5Weapon {
		WeaponRarity = append(WeaponRarity, 5)
	}
	if opts.Rarity4Weapon {
		WeaponRarity = append(WeaponRarity, 4)
	}
	var essenceTypes []EssenceMeta
	if opts.FlawlessEssence {
		essenceTypes = append(essenceTypes, FlawlessEssenceMeta)
	}
	if opts.PureEssence {
		essenceTypes = append(essenceTypes, PureEssenceMeta)
	}
	if len(essenceTypes) == 0 {
		log.Error().Str("component", "EssenceFilter").Str("step", "ValidatePresets").Msg("no essence type selected")
		LogMXUSimpleHTMLWithColor(ctx, "未选择任何基质类型，请至少选择一个基质类型作为筛选条件", "#ff0000")
		return false
	}

	if len(WeaponRarity) == 0 {
		LogMXUSimpleHTML(ctx, "未选择武器稀有度，仅使用扩展规则")
	} else {
		LogMXUSimpleHTML(ctx, fmt.Sprintf("已选择稀有度：%s", rarityListToString(WeaponRarity)))
	}
	LogMXUSimpleHTML(ctx, fmt.Sprintf("已选择基质类型：%s", essenceListToString(essenceTypes)))
	filteredWeapons := FilterWeaponsByConfig(WeaponRarity)
	names := make([]string, 0, len(filteredWeapons))
	for _, w := range filteredWeapons {
		names = append(names, w.ChineseName)
	}
	log.Info().Str("component", "EssenceFilter").Str("step", "FilterWeapons").Int("filtered_count", len(filteredWeapons)).Strs("weapons", names).Msg("weapons filtered")

	st := &RunState{MaxItemsPerRow: 9, EssenceTypes: essenceTypes}
	st.Reset()
	st.TargetSkillCombinations = ExtractSkillCombinations(filteredWeapons)
	st.MatchedCombinationSummary = make(map[string]*SkillCombinationSummary)
	st.EssenceTypes = essenceTypes
	setRunState(st)
	buildFilteredSkillStats(filteredWeapons)

	if len(filteredWeapons) == 0 {
		LogMXUSimpleHTML(ctx, "符合条件的武器数量：0（仅扩展规则）")
	} else {
		LogMXUSimpleHTML(ctx, fmt.Sprintf("符合条件的武器数量：%d", len(filteredWeapons)))
	}
	sort.Slice(filteredWeapons, func(i, j int) bool { return filteredWeapons[i].Rarity > filteredWeapons[j].Rarity })
	if len(filteredWeapons) > 0 {
		var builder strings.Builder
		const columns = 3
		builder.WriteString(`<table style="width: 100%; border-collapse: collapse;">`)
		for i, w := range filteredWeapons {
			if i%columns == 0 {
				builder.WriteString("<tr>")
			}
			builder.WriteString(fmt.Sprintf(`<td style="padding: 2px 8px; color: %s; font-size: 11px;">%s</td>`, getColorForRarity(w.Rarity), w.ChineseName))
			if i%columns == columns-1 || i == len(filteredWeapons)-1 {
				builder.WriteString("</tr>")
			}
		}
		builder.WriteString("</table>")
		LogMXUHTML(ctx, builder.String())
	} else {
		LogMXUSimpleHTML(ctx, "未选择武器，无目标武器列表")
	}

	log.Info().Str("component", "EssenceFilter").Str("step", "BuildSkillCombinations").Int("combinations", len(st.TargetSkillCombinations)).Msg("skill combinations built")
	log.Info().Str("component", "EssenceFilter").Msg("init done")

	if len(st.TargetSkillCombinations) > 0 {
		const columns = 3
		var skillIdSlots [3][]int
		for _, c := range st.TargetSkillCombinations {
			for i, skillID := range c.SkillIDs {
				skillIdSlots[i] = append(skillIdSlots[i], skillID)
			}
		}
		var skillBuilder strings.Builder
		skillBuilder.WriteString(`<div style="color: #00bfff; font-weight: 900;">目标技能列表：</div>`)
		slotColors := []string{"#47b5ff", "#11dd11", "#e877fe"}
		for i, idSlot := range skillIdSlots {
			uniqueIds := make(map[int]struct{})
			for _, id := range idSlot {
				uniqueIds[id] = struct{}{}
			}
			pool := GetPoolBySlot(i + 1)
			skillNames := make([]string, 0, len(uniqueIds))
			for id := range uniqueIds {
				skillNames = append(skillNames, SkillNameByID(id, pool))
			}
			sort.Strings(skillNames)
			if len(skillNames) == 0 {
				continue
			}
			slotColor := slotColors[i]
			skillBuilder.WriteString(fmt.Sprintf(`<div style="color: %s; font-weight: 700;">词条 %d:</div>`, slotColor, i+1))
			skillBuilder.WriteString(fmt.Sprintf(`<table style="width: 100%%; color: %s; border-collapse: collapse;">`, slotColor))
			for j, name := range skillNames {
				if j%columns == 0 {
					skillBuilder.WriteString("<tr>")
				}
				skillBuilder.WriteString(fmt.Sprintf(`<td style="padding: 2px 8px; font-size: 12px;">%s</td>`, name))
				if j%columns == columns-1 || j == len(skillNames)-1 {
					skillBuilder.WriteString("</tr>")
				}
			}
			skillBuilder.WriteString("</table>")
		}
		LogMXUHTML(ctx, skillBuilder.String())
	} else {
		LogMXUSimpleHTML(ctx, "未选择武器，无目标技能列表")
	}
	return true
}

// --- OCR 库存数量 / Trace（同一 case：轻量辅助 action）---

// OCREssenceInventoryNumberAction - OCR inventory count and override next if single page
type OCREssenceInventoryNumberAction struct{}

func (a *OCREssenceInventoryNumberAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	const maxSinglePage = 45
	if arg.RecognitionDetail == nil || arg.RecognitionDetail.Results == nil {
		log.Error().Str("component", "EssenceFilter").Str("action", "CheckTotal").Msg("no OCR detail")
		return false
	}
	var text string
	for _, results := range [][]*maa.RecognitionResult{{arg.RecognitionDetail.Results.Best}, arg.RecognitionDetail.Results.Filtered, arg.RecognitionDetail.Results.All} {
		if len(results) > 0 {
			if ocrResult, ok := results[0].AsOCR(); ok && strings.TrimSpace(ocrResult.Text) != "" {
				text = strings.TrimSpace(ocrResult.Text)
				break
			}
		}
	}
	if text == "" {
		log.Error().Str("component", "EssenceFilter").Str("action", "CheckTotal").Msg("OCR text empty")
		return false
	}
	re := regexp.MustCompile(`\d+`)
	nums := re.FindAllString(text, -1)
	if len(nums) == 0 {
		log.Error().Str("component", "EssenceFilter").Str("action", "CheckTotal").Str("text", text).Msg("no number found")
		return false
	}
	n, err := strconv.Atoi(nums[0])
	if err != nil {
		log.Error().Err(err).Str("component", "EssenceFilter").Str("action", "CheckTotal").Str("text", text).Msg("parse failed")
		return false
	}
	log.Info().Str("component", "EssenceFilter").Str("action", "CheckTotal").Int("count", n).Int("max_single_page", maxSinglePage).Str("raw", text).Msg("total parsed")
	msg := fmt.Sprintf("库存中共 <span style=\"color: #ff7000; font-weight: 900;\">%d</span> 个基质", n)
	if v := GetMatcherConfig().DataVersion; v != "" {
		msg += fmt.Sprintf(" <span style=\"color: #ff0000;\">当前数据日期：%s</span>(如果更新了请注意)", v)
	}
	LogMXUSimpleHTML(ctx, msg)
	if st := getRunState(); st != nil {
		st.TotalCount = n
	}
	if n <= maxSinglePage {
		ctx.OverrideNext(arg.CurrentTaskName, []maa.NextItem{{Name: "EssenceDetectFinal"}})
	}
	return true
}

// EssenceFilterTraceAction - log node/step
type EssenceFilterTraceAction struct{}

func (a *EssenceFilterTraceAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	var params struct {
		Step string `json:"step"`
	}
	_ = json.Unmarshal([]byte(arg.CustomActionParam), &params)
	if params.Step == "" {
		params.Step = arg.CurrentTaskName
	}
	log.Info().Str("component", "EssenceFilter").Str("step", params.Step).Str("node", arg.CurrentTaskName).Msg("trace")
	return true
}

// --- CheckItem / CheckItemLevel / SkillDecision（同一 case：单格技能识别与决策）---

// EssenceFilterCheckItemAction - OCR skills and match
type EssenceFilterCheckItemAction struct{}

func (a *EssenceFilterCheckItemAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	log.Info().Str("component", "EssenceFilter").Str("action", "CheckItem").Msg("start")
	st := getRunState()
	if st == nil {
		log.Error().Str("component", "EssenceFilter").Str("action", "CheckItem").Msg("no run state")
		return false
	}
	if !st.StatsLogged {
		logFilteredSkillStats()
		st.StatsLogged = true
	}
	var params struct {
		Slot   int  `json:"slot"`
		IsLast bool `json:"is_last"`
	}
	if arg.CustomActionParam != "" {
		_ = json.Unmarshal([]byte(arg.CustomActionParam), &params)
	}
	if params.Slot < 1 || params.Slot > 3 {
		log.Error().Str("component", "EssenceFilter").Int("slot", params.Slot).Msg("invalid slot param")
		return false
	}
	if params.Slot == 1 {
		st.CurrentSkills = [3]string{}
		st.CurrentSkillLevels = [3]int{}
	}
	if arg.RecognitionDetail == nil || arg.RecognitionDetail.Results == nil {
		log.Error().Str("component", "EssenceFilter").Msg("OCR detail missing from pipeline")
		return false
	}
	var rawText string
	for _, results := range [][]*maa.RecognitionResult{{arg.RecognitionDetail.Results.Best}, arg.RecognitionDetail.Results.Filtered, arg.RecognitionDetail.Results.All} {
		if len(results) > 0 {
			if ocrResult, ok := results[0].AsOCR(); ok && ocrResult.Text != "" {
				rawText = ocrResult.Text
				break
			}
		}
	}
	text := cleanChinese(rawText)
	if text == "" {
		log.Error().Str("component", "EssenceFilter").Int("slot", params.Slot).Str("raw", rawText).Msg("OCR empty")
		return false
	}
	st.CurrentSkills[params.Slot-1] = text
	log.Info().Str("component", "EssenceFilter").Int("slot", params.Slot).Str("skill", rawText).Bool("is_last", params.IsLast).Msg("OCR ok")
	if !params.IsLast {
		return true
	}
	for i, s := range st.CurrentSkills {
		if s == "" {
			log.Error().Str("component", "EssenceFilter").Int("slot", i+1).Msg("missing skill for slot")
			return false
		}
	}
	return true
}

// EssenceFilterCheckItemLevelAction - 识别技能等级（独立 level ROI）
type EssenceFilterCheckItemLevelAction struct{}

func (a *EssenceFilterCheckItemLevelAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	var params struct {
		Slot int `json:"slot"`
	}
	if arg.CustomActionParam != "" {
		_ = json.Unmarshal([]byte(arg.CustomActionParam), &params)
	}
	if params.Slot < 1 || params.Slot > 3 {
		log.Error().Str("component", "EssenceFilter").Int("slot", params.Slot).Msg("invalid level slot param")
		return false
	}
	if arg.RecognitionDetail == nil || arg.RecognitionDetail.Results == nil {
		log.Error().Str("component", "EssenceFilter").Int("slot", params.Slot).Msg("level OCR detail missing")
		return false
	}
	var rawText string
	for _, results := range [][]*maa.RecognitionResult{{arg.RecognitionDetail.Results.Best}, arg.RecognitionDetail.Results.Filtered, arg.RecognitionDetail.Results.All} {
		if len(results) > 0 {
			if ocrResult, ok := results[0].AsOCR(); ok && strings.TrimSpace(ocrResult.Text) != "" {
				rawText = strings.TrimSpace(ocrResult.Text)
				break
			}
		}
	}
	if rawText == "" {
		log.Error().Str("component", "EssenceFilter").Int("slot", params.Slot).Msg("level OCR empty")
		return false
	}
	st := getRunState()
	if st == nil {
		return false
	}
	if m := levelParseRe.FindStringSubmatch(rawText); len(m) >= 2 {
		if lv, err := strconv.Atoi(m[1]); err == nil && lv >= 1 && lv <= 6 {
			st.CurrentSkillLevels[params.Slot-1] = lv
			log.Info().Str("component", "EssenceFilter").Int("slot", params.Slot).Int("level", lv).Str("raw", rawText).Msg("OCR level ok")
			return true
		}
	}
	log.Error().Str("component", "EssenceFilter").Int("slot", params.Slot).Str("raw", rawText).Msg("level parse fail")
	return false
}

// EssenceFilterSkillDecisionAction - match skills then decide lock or skip
type EssenceFilterSkillDecisionAction struct{}

func (a *EssenceFilterSkillDecisionAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	st := getRunState()
	if st == nil {
		return false
	}
	skills := []string{st.CurrentSkills[0], st.CurrentSkills[1], st.CurrentSkills[2]}
	opts, _ := getOptionsFromAttach(ctx, "EssenceFilterInit")
	if opts == nil {
		opts = &EssenceFilterOptions{}
	}
	matchResult, matched := MatchEssenceSkills(ctx, skills, st.TargetSkillCombinations)
	extendedReason := ""
	shouldLockExtended := false
	if !matched && opts != nil {
		if opts.KeepFuturePromising && opts.FuturePromisingMinTotal > 0 {
			if MatchFuturePromising(skills, st.CurrentSkillLevels, opts.FuturePromisingMinTotal) {
				matched = true
				shouldLockExtended = opts.LockFuturePromising
				sum := st.CurrentSkillLevels[0] + st.CurrentSkillLevels[1] + st.CurrentSkillLevels[2]
				matchResult = &SkillCombinationMatch{
					SkillIDs:      []int{0, 0, 0},
					SkillsChinese: []string{skills[0], skills[1], skills[2]},
					Weapons:       []WeaponData{},
				}
				extendedReason = fmt.Sprintf("未来可期：总等级 %d ≥ %d", sum, opts.FuturePromisingMinTotal)
				st.ExtFuturePromisingCount++
				log.Info().Str("component", "EssenceFilter").Str("rule", "MatchFuturePromising").Strs("skills", skills).Ints("levels", st.CurrentSkillLevels[:]).Int("sum", sum).Int("min_total", opts.FuturePromisingMinTotal).Msg("keep future promising essence")
			}
		}
		slot3MinLv := opts.Slot3MinLevel
		if slot3MinLv <= 0 {
			slot3MinLv = 3
		}
		if !matched && opts.KeepSlot3Level3Practical {
			var slot3Lv int
			var slot3Match bool
			matchResult, slot3Lv, slot3Match = MatchSlot3Level3Practical(skills, st.CurrentSkillLevels, slot3MinLv)
			if slot3Match {
				matched = true
				shouldLockExtended = opts.LockSlot3Practical
				extendedReason = fmt.Sprintf("实用基质：词条3(%s)等级 %d ≥ %d", matchResult.SkillsChinese[2], slot3Lv, slot3MinLv)
				st.ExtSlot3PracticalCount++
				log.Info().Str("component", "EssenceFilter").Str("rule", "MatchSlot3Level3Practical").Str("slot3_skill", matchResult.SkillsChinese[2]).Int("slot3_level", slot3Lv).Int("min_level", slot3MinLv).Msg("keep practical essence")
			}
		}
	}
	MatchedMessageColor := "#00bfff"
	if matched {
		MatchedMessageColor = "#064d7c"
	}
	LogMXUSimpleHTMLWithColor(ctx, fmt.Sprintf("OCR到技能：%s(+%d) | %s(+%d) | %s(+%d)", skills[0], st.CurrentSkillLevels[0], skills[1], st.CurrentSkillLevels[1], skills[2], st.CurrentSkillLevels[2]), MatchedMessageColor)

	if matched && extendedReason != "" {
		if shouldLockExtended {
			st.MatchedCount++
			log.Info().Str("component", "EssenceFilter").Strs("skills", skills).Str("reason", extendedReason).Int("matched_count", st.MatchedCount).Msg("extended rule hit, lock next")
			LogMXUHTML(ctx, fmt.Sprintf(`<div style="color: #064d7c; font-weight: 900;">🔒 扩展规则命中并锁定：%s</div>`, escapeHTML(extendedReason)))
			ctx.OverrideNext(arg.CurrentTaskName, []maa.NextItem{{Name: "EssenceFilterLockItemLog"}})
		} else {
			log.Info().Str("component", "EssenceFilter").Strs("skills", skills).Str("reason", extendedReason).Msg("extended rule hit, no operation")
			LogMXUHTML(ctx, fmt.Sprintf(`<div style="color: #d18b00; font-weight: 900;">🗂️ 扩展规则命中（不操作）：%s</div>`, escapeHTML(extendedReason)))
			ctx.OverrideNext(arg.CurrentTaskName, []maa.NextItem{{Name: "EssenceFilterRowNextItem"}})
		}
	} else if matched {
		st.MatchedCount++
		weaponNames := make([]string, 0, len(matchResult.Weapons))
		for _, w := range matchResult.Weapons {
			weaponNames = append(weaponNames, w.ChineseName)
		}
		log.Info().Str("component", "EssenceFilter").Strs("weapons", weaponNames).Strs("skills", skills).Ints("skill_ids", matchResult.SkillIDs).Int("matched_count", st.MatchedCount).Msg("match ok, lock next")
		var weaponsHTML strings.Builder
		for i, w := range matchResult.Weapons {
			if i > 0 {
				weaponsHTML.WriteString("、")
			}
			weaponsHTML.WriteString(fmt.Sprintf(`<span style="color: %s;">%s</span>`, getColorForRarity(w.Rarity), escapeHTML(w.ChineseName)))
		}
		LogMXUHTML(ctx, fmt.Sprintf(`<div style="color: #064d7c; font-weight: 900;">匹配到武器：%s</div>`, weaponsHTML.String()))
		key := skillCombinationKey(matchResult.SkillIDs)
		if key != "" {
			if s, ok := st.MatchedCombinationSummary[key]; ok {
				s.Count++
			} else {
				st.MatchedCombinationSummary[key] = &SkillCombinationSummary{
					SkillIDs:      append([]int(nil), matchResult.SkillIDs...),
					SkillsChinese: append([]string(nil), matchResult.SkillsChinese...),
					OCRSkills:     append([]string(nil), skills...),
					Weapons:       append([]WeaponData(nil), matchResult.Weapons...),
					Count:         1,
				}
			}
		}
		ctx.OverrideNext(arg.CurrentTaskName, []maa.NextItem{{Name: "EssenceFilterLockItemLog"}})
	} else {
		if opts.DiscardUnmatched {
			log.Info().Str("component", "EssenceFilter").Strs("skills", skills).Msg("not matched, discard item")
			LogMXUHTML(ctx, `<div style="color: #ff6b6b; font-weight: 900;">🗑️ 未匹配到目标技能组合，废弃该物品</div>`)
			ctx.OverrideNext(arg.CurrentTaskName, []maa.NextItem{{Name: "EssenceFilterDiscardItemLog"}})
		} else {
			log.Info().Str("component", "EssenceFilter").Strs("skills", skills).Msg("not matched, skip to next item")
			LogMXUSimpleHTML(ctx, "未匹配到目标技能组合，跳过该物品")
			ctx.OverrideNext(arg.CurrentTaskName, []maa.NextItem{{Name: "EssenceFilterRowNextItem"}})
		}
	}
	st.CurrentSkills = [3]string{}
	st.CurrentSkillLevels = [3]int{}
	return true
}

// --- RowCollect / RowNextItem / Finish / SwipeCalibrate（同一 case：行遍历与网格）---

// EssenceFilterRowCollectAction - collect boxes in a row (TemplateMatch + ColorMatch), then RowNextItem
type EssenceFilterRowCollectAction struct{}

func (a *EssenceFilterRowCollectAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if arg.RecognitionDetail == nil || arg.RecognitionDetail.Results == nil || !arg.RecognitionDetail.Hit {
		log.Error().Str("component", "EssenceFilter").Str("action", "RowCollect").Msg("recognition detail empty")
		return false
	}
	st := getRunState()
	if st == nil {
		return false
	}
	results := arg.RecognitionDetail.Results.Filtered
	if len(results) == 0 {
		results = arg.RecognitionDetail.Results.All
	}
	controller := ctx.GetTasker().GetController()
	if controller == nil {
		log.Error().Str("component", "EssenceFilter").Str("action", "RowCollect").Msg("controller nil")
		return false
	}
	controller.PostScreencap().Wait()
	img, err := controller.CacheImage()
	if err != nil {
		log.Error().Err(err).Str("component", "EssenceFilter").Str("action", "RowCollect").Msg("get screenshot failed")
		return false
	}
	st.RowBoxes = st.RowBoxes[:0]
	st.PhysicalItemCount = len(results)

	opts, _ := getOptionsFromAttach(ctx, "EssenceFilterInit")
	skipMarked := false
	if opts != nil {
		skipMarked = opts.SkipLockedRow
	}

	for _, res := range results {
		tm, ok := res.AsTemplateMatch()
		if !ok {
			continue
		}
		b := tm.Box
		boxArr := [4]int{b.X(), b.Y(), b.Width(), b.Height()}
		colorMatchROIW := boxArr[2]
		colorMatchROIH := boxArr[3] - 90
		if colorMatchROIW <= 0 || colorMatchROIH <= 0 {
			continue
		}
		roi := maa.Rect{boxArr[0], boxArr[1] + 90, colorMatchROIW, colorMatchROIH}

		colorMatched := false
		for _, et := range st.EssenceTypes {
			cDetail, err := ctx.RunRecognition("EssenceColorMatch", img, map[string]any{
				// 直接传递 roi 切片
				"EssenceColorMatch": map[string]any{"roi": roi, "lower": et.Range.Lower, "upper": et.Range.Upper},
			})
			if err != nil {
				continue
			}
			if cDetail != nil && cDetail.Hit {
				colorMatched = true
				break
			}
		}

		if colorMatched {
			isMarked := false
			if skipMarked {
				margin := 10
				bx1, by1 := boxArr[0]-margin, boxArr[1]-margin
				if bx1 < 0 {
					bx1 = 0
				}
				if by1 < 0 {
					by1 = 0
				}
				bw, bh := boxArr[2]+margin*2, boxArr[3]+margin*2

				roiX := bx1
				roiY := by1 + int(float64(bh)*0.65)
				roiW := int(float64(bw) * 0.30)
				roiH := int(float64(bh) * 0.35)

				thumbDetail, err := ctx.RunRecognition("EssenceThumbMarked", img, map[string]any{
					"EssenceThumbMarked": map[string]any{
						"roi": []int{roiX, roiY, roiW, roiH},
					},
				})
				if err == nil && thumbDetail != nil && thumbDetail.Hit {
					isMarked = true
				}
			}

			if !isMarked {
				st.RowBoxes = append(st.RowBoxes, boxArr)
			}
		}
	}

	sort.Slice(st.RowBoxes, func(i, j int) bool {
		if st.RowBoxes[i][1] == st.RowBoxes[j][1] {
			return st.RowBoxes[i][0] < st.RowBoxes[j][0]
		}
		return st.RowBoxes[i][1] < st.RowBoxes[j][1]
	})

	log.Info().Str("component", "EssenceFilter").Str("action", "RowCollect").Int("len_results", len(results)).Int("valid_boxes", len(st.RowBoxes)).Msg("color match done")

	if skipMarked && len(st.RowBoxes) == 0 && st.PhysicalItemCount == st.MaxItemsPerRow {
		LogMXUSimpleHTMLWithColor(ctx, fmt.Sprintf("第 %d 行已全部标记，跳过", st.CurrentRow), "#11cf00")
	}

	isFallbackScan := arg.CurrentTaskName == "EssenceDetectFinal"
	st.InFinalScan = isFallbackScan
	if isFallbackScan && !st.FinalLargeScanUsed {
		st.FinalLargeScanUsed = true
		ctx.OverrideNext(arg.CurrentTaskName, []maa.NextItem{{Name: "EssenceDetectFinal"}})
		LogMXUSimpleHTMLWithColor(ctx, "尾扫完成，收集所有剩余基质格子", "#1a01fd")
		return true
	}
	if (st.PhysicalItemCount > st.MaxItemsPerRow) && !isFallbackScan {
		ctx.OverrideNext(arg.CurrentTaskName, []maa.NextItem{{Name: "EssenceFilterFinish"}})
		return true
	}
	if st.PhysicalItemCount == 0 {
		ctx.OverrideNext(arg.CurrentTaskName, []maa.NextItem{{Name: "EssenceFilterFinish"}})
		return true
	}
	st.RowIndex = 0
	ctx.OverrideNext(arg.CurrentTaskName, []maa.NextItem{{Name: "EssenceFilterRowNextItem"}})
	return true
}

// EssenceFilterRowNextItemAction - proceed to next box or swipe/finish
type EssenceFilterRowNextItemAction struct{}

func (a *EssenceFilterRowNextItemAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	st := getRunState()
	if st == nil {
		return false
	}
	if st.PendingFinalScan {
		st.PendingFinalScan = false
		// 尾扫后直接基于已收集的 RowBoxes 逐个处理，不再尝试 TryLastFirst/回 EssenceRowDetect
		st.InFinalScan = true
		st.TryLastFirst = false
		log.Info().Str("component", "EssenceFilter").Str("action", "RowNextItem").Msg("补 swipe 完成，进入尾扫")
		LogMXUSimpleHTML(ctx, "补 swipe 完成，进入尾扫")
		ctx.OverrideNext(arg.CurrentTaskName, []maa.NextItem{{Name: "EssenceDetectFinal"}})
		return true
	}
	// Try-last-first: only when row is full (9). If total known and remaining this row < 9, skip and use normal logic.
	if st.RowIndex == 0 && st.TryLastFirst && !st.InFinalScan && len(st.RowBoxes) > 0 {
		remaining := st.TotalCount - st.MaxItemsPerRow*(st.CurrentRow-1)
		if st.TotalCount > 0 && remaining < st.MaxItemsPerRow {
			// partial row, do not try last first
		} else {
			rowCopy := make([][4]int, len(st.RowBoxes))
			copy(rowCopy, st.RowBoxes)
			sort.Slice(rowCopy, func(i, j int) bool { return rowCopy[i][0] < rowCopy[j][0] })
			lastBox := rowCopy[len(rowCopy)-1]
			clickingBox := [4]int{lastBox[0] + 10, lastBox[1] + 10, lastBox[2] - 20, lastBox[3] - 20}
			log.Info().Str("component", "EssenceFilter").Str("action", "RowNextItem").Str("mode", "try_last_first").Ints("box", lastBox[:]).Msg("click last box (x-sorted rightmost) to check row locked")
			ctx.RunTask("NodeClick", map[string]any{
				"NodeClick": map[string]any{
					"action": map[string]any{"param": map[string]any{"target": clickingBox}},
				},
			})
			controller := ctx.GetTasker().GetController()
			if controller == nil {
				log.Error().Str("component", "EssenceFilter").Str("action", "RowNextItem").Msg("controller nil")
				return false
			}
			controller.PostScreencap().Wait()
			img, err := controller.CacheImage()
			if err != nil {
				log.Error().Err(err).Str("component", "EssenceFilter").Str("action", "RowNextItem").Msg("get screenshot failed")
				st.TryLastFirst = false
				ctx.OverrideNext(arg.CurrentTaskName, []maa.NextItem{{Name: "EssenceRowDetect"}})
				return true
			}
			detail, err := ctx.RunRecognition("EssenceFilterRecognitionLocked", img, nil)
			log.Info().Str("component", "EssenceFilter").Str("action", "RowNextItem").Err(err).Bool("hit", detail != nil && detail.Hit).Interface("detail", detail).Msg("RunRecognition EssenceFilterRecognitionLocked result")
			if err != nil || detail == nil || !detail.Hit {
				st.TryLastFirst = false
				log.Info().Str("component", "EssenceFilter").Str("action", "RowNextItem").Msg("last item not locked, re-run row from first")
				ctx.OverrideNext(arg.CurrentTaskName, []maa.NextItem{{Name: "EssenceRowDetect"}})
				return true
			}
			st.RowIndex = len(st.RowBoxes)
			ctx.OverrideNext(arg.CurrentTaskName, []maa.NextItem{{Name: "EssenceFilterRowNextItem"}})
			return true
		}
	}
	if st.RowIndex >= len(st.RowBoxes) {
		if (st.PhysicalItemCount == st.MaxItemsPerRow) && !st.FinalLargeScanUsed {
			const maxRemainingForFinalScan = 45
			rowsDone := st.CurrentRow
			remaining := st.TotalCount - st.MaxItemsPerRow*rowsDone
			if st.TotalCount > 0 && remaining <= maxRemainingForFinalScan {
				st.PendingFinalScan = true
				LogMXUSimpleHTML(ctx, fmt.Sprintf("剩余 %d 个 ≤ %d，先补一次滑动再尾扫（总 %d，已 %d 行）", remaining, maxRemainingForFinalScan, st.TotalCount, rowsDone))
			}
			nextNode := "EssenceFilterSwipeNext"
			if !st.FirstRowSwipeDone {
				st.FirstRowSwipeDone = true
				nextNode = "EssenceFilterSwipeFirst"
			}
			// 最后一次补滑（remaining <= 45）不走校准：避免 SwipeCalibrate 识别失败导致流程中断
			if st.PendingFinalScan {
				if nextNode == "EssenceFilterSwipeFirst" {
					nextNode = "EssenceFilterSwipeFirstNoCalibrate"
				} else {
					nextNode = "EssenceFilterSwipeNextNoCalibrate"
				}
			}
			ctx.OverrideNext(arg.CurrentTaskName, []maa.NextItem{{Name: nextNode}})
			LogMXUSimpleHTML(ctx, fmt.Sprintf("滑动到第 %d 行", st.CurrentRow+1))
			st.CurrentRow++
			return true
		}
		ctx.OverrideNext(arg.CurrentTaskName, []maa.NextItem{{Name: "EssenceFilterFinish"}})
		return true
	}

	box := st.RowBoxes[st.RowIndex]
	log.Info().Str("component", "EssenceFilter").Str("action", "RowNextItem").Ints("box", box[:]).Msg("click next box")
	clickingBox := [4]int{box[0] + 10, box[1] + 10, box[2] - 20, box[3] - 20}
	ctx.RunTask("NodeClick", map[string]any{
		"NodeClick": map[string]any{
			"action": map[string]any{"param": map[string]any{"target": clickingBox}},
		},
	})
	st.VisitedCount++
	st.RowIndex++
	ctx.OverrideNext(arg.CurrentTaskName, []maa.NextItem{{Name: "EssenceFilterCheckItemSlot1"}})
	return true
}

// EssenceFilterFinishAction - finish and reset
type EssenceFilterFinishAction struct{}

func (a *EssenceFilterFinishAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	log.Info().Str("component", "EssenceFilter").Msg("finish")
	st := getRunState()
	if st != nil {
		log.Info().Str("component", "EssenceFilter").Int("matched_total", st.MatchedCount).Msg("locked items")
		LogMXUSimpleHTMLWithColor(ctx, fmt.Sprintf("筛选完成！共历遍物品：%d，确认锁定物品：%d", st.VisitedCount, st.MatchedCount), "#11cf00")
		logMatchSummary(ctx)
		opts, _ := getOptionsFromAttach(ctx, "EssenceFilterInit")
		if opts != nil {
			if opts.KeepFuturePromising {
				LogMXUSimpleHTMLWithColor(ctx, fmt.Sprintf("扩展规则「未来可期」命中：%d 个", st.ExtFuturePromisingCount), "#064d7c")
			}
			if opts.KeepSlot3Level3Practical {
				LogMXUSimpleHTMLWithColor(ctx, fmt.Sprintf("扩展规则「实用基质」命中：%d 个", st.ExtSlot3PracticalCount), "#064d7c")
			}
			if opts.ExportCalculatorScript {
				logCalculatorResult(ctx)
			}
		}
	}
	setRunState(nil)
	return true
}

const firstRowTargetY = 86       //首行Y
const calibrateTolerance = 8     //校准误差
const calibrateScrollRatio = 1.1 //校准滑动比例
const calibrateSwipeMin = 8      //校准滑动最小值
const calibrateSwipeMax = 40     //校准滑动最大值

// EssenceFilterSwipeCalibrateAction - 根据首个 box 的 Y 校准到基准 firstRowTargetY
type EssenceFilterSwipeCalibrateAction struct{}

func (a *EssenceFilterSwipeCalibrateAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	st := getRunState()
	if st == nil {
		ctx.OverrideNext(arg.CurrentTaskName, []maa.NextItem{{Name: "EssenceRowDetect"}, {Name: "EssenceDetectFinal"}})
		return true
	}
	if st.SwipeCalibrateRetry >= 5 {
		st.SwipeCalibrateRetry = 0
		ctx.OverrideNext(arg.CurrentTaskName, []maa.NextItem{{Name: "EssenceRowDetect"}, {Name: "EssenceDetectFinal"}})
		return true
	}
	if arg.RecognitionDetail == nil || arg.RecognitionDetail.Results == nil || !arg.RecognitionDetail.Hit {
		ctx.OverrideNext(arg.CurrentTaskName, []maa.NextItem{{Name: "EssenceRowDetect"}, {Name: "EssenceDetectFinal"}})
		return true
	}
	results := arg.RecognitionDetail.Results.Filtered
	if len(results) == 0 {
		results = arg.RecognitionDetail.Results.All
	}
	if len(results) == 0 {
		ctx.OverrideNext(arg.CurrentTaskName, []maa.NextItem{{Name: "EssenceRowDetect"}, {Name: "EssenceDetectFinal"}})
		return true
	}
	boxes := make([][4]int, 0, len(results))
	for _, res := range results {
		tm, ok := res.AsTemplateMatch()
		if !ok {
			continue
		}
		b := tm.Box
		boxes = append(boxes, [4]int{b.X(), b.Y(), b.Width(), b.Height()})
	}
	sort.Slice(boxes, func(i, j int) bool { return boxes[i][0] < boxes[j][0] })
	firstBoxY := boxes[0][1]
	if firstBoxY >= firstRowTargetY-calibrateTolerance && firstBoxY <= firstRowTargetY+calibrateTolerance {
		st.SwipeCalibrateRetry = 0
		ctx.OverrideNext(arg.CurrentTaskName, []maa.NextItem{{Name: "EssenceRowDetect"}, {Name: "EssenceDetectFinal"}})
		return true
	}
	delta := firstBoxY - firstRowTargetY
	swipeDist := int(float64(abs(delta)) * calibrateScrollRatio)
	if swipeDist < calibrateSwipeMin {
		swipeDist = calibrateSwipeMin
	}
	if swipeDist > calibrateSwipeMax {
		swipeDist = calibrateSwipeMax
	}
	centerX, beginY := 135, 191
	var endY int
	if delta > 0 {
		endY = beginY - swipeDist
	} else {
		endY = beginY + swipeDist
	}
	ctx.RunTask("EssenceFilterSwipeCalibrateCorrect", map[string]any{
		"EssenceFilterSwipeCalibrateCorrect": map[string]any{
			"action": map[string]any{"param": map[string]any{"begin": []int{centerX, beginY}, "end": []int{centerX, endY}}},
		},
	})
	st.SwipeCalibrateRetry++
	ctx.OverrideNext(arg.CurrentTaskName, []maa.NextItem{{Name: "EssenceFilterSwipeCalibrate"}})
	return true
}
