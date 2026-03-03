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

// EssenceFilterInitAction - initialize filter
type EssenceFilterInitAction struct{}

func (a *EssenceFilterInitAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	log.Info().Msg("<EssenceFilter> ========== Init ==========")

	base := getResourceBase()
	if base == "" {
		base = "data" // fallback to current relative default
	}

	gameDataDir := filepath.Join(base, "EssenceFilter")
	weaponDataPath = filepath.Join(gameDataDir, "weapons_data.json")
	matcherConfigPath := filepath.Join(gameDataDir, "matcher_config.json")

	// 2. load matcher config
	if err := LoadMatcherConfig(matcherConfigPath); err != nil {
		log.Error().Err(err).Msg("<EssenceFilter> Step2 failed: load matcher config")
		return false
	}
	log.Info().Msg("<EssenceFilter> Step2 ok: matcher config loaded")

	// 3. load DB
	if err := LoadWeaponDatabase(weaponDataPath); err != nil {
		log.Error().Err(err).Msg("<EssenceFilter> Step3 failed: load DB")
		return false
	}
	LogMXUSimpleHTML(ctx, "武器数据加载完成")
	logSkillPools()

	// 4. load presets
	opts, err := getOptionsFromAttach(ctx, arg.CurrentTaskName)
	if err != nil {
		log.Error().Err(err).Msg("<EssenceFilter> Step4 failed: load options")
		return false
	}

	// 5. select preset

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

	if len(WeaponRarity) == 0 {
		log.Error().Msg("<EssenceFilter> Step5 failed: no preset selected, please select at least one preset")
		LogMXUSimpleHTMLWithColor(ctx, "未选择任何武器稀有度，请至少选择一个武器稀有度作为筛选条件", "#ff0000")
		return false
	}

	EssenceTypes = EssenceTypes[:0] // reset global EssenceTypes slice
	if opts.FlawlessEssence {
		EssenceTypes = append(EssenceTypes, FlawlessEssenceMeta)
	}
	if opts.PureEssence {
		EssenceTypes = append(EssenceTypes, PureEssenceMeta)
	}

	if len(EssenceTypes) == 0 {
		log.Error().Msg("<EssenceFilter> Step5 failed: no essence type selected, please select at least one essence type")
		LogMXUSimpleHTMLWithColor(ctx, "未选择任何基质类型，请至少选择一个基质类型作为筛选条件", "#ff0000")
		return false
	}

	LogMXUSimpleHTML(ctx, fmt.Sprintf("已选择稀有度：%s", rarityListToString(WeaponRarity)))
	LogMXUSimpleHTML(ctx, fmt.Sprintf("已选择基质类型：%s", essenceListToString(EssenceTypes)))
	// 6. filter weapons
	filteredWeapons := FilterWeaponsByConfig(WeaponRarity)
	names := make([]string, 0, len(filteredWeapons))
	for _, w := range filteredWeapons {
		names = append(names, w.ChineseName)
	}
	log.Info().Int("filtered_count", len(filteredWeapons)).Strs("weapons", names).Msg("<EssenceFilter> Step6 ok")
	buildFilteredSkillStats(filteredWeapons)
	LogMXUSimpleHTML(ctx, fmt.Sprintf("符合条件的武器数量：%d", len(filteredWeapons)))
	// Construct weapon list in HTML to show
	sort.Slice(filteredWeapons, func(i, j int) bool {
		return filteredWeapons[i].Rarity > filteredWeapons[j].Rarity
	})
	var builder strings.Builder
	const columns = 3
	builder.WriteString(`<table style="width: 100%; border-collapse: collapse;">`)
	for i, w := range filteredWeapons {
		if i%columns == 0 {
			builder.WriteString("<tr>")
		}
		color := getColorForRarity(w.Rarity)
		builder.WriteString(fmt.Sprintf(`<td style="padding: 2px 8px; color: %s; font-size: 11px;">%s</td>`, color, w.ChineseName))
		if i%columns == columns-1 || i == len(filteredWeapons)-1 {
			builder.WriteString("</tr>")
		}
	}
	builder.WriteString("</table>")
	LogMXUHTML(ctx, builder.String())

	// 7. extract combos
	targetSkillCombinations = ExtractSkillCombinations(filteredWeapons)
	visitedCount = 0
	matchedCount = 0
	matchedCombinationSummary = make(map[string]*SkillCombinationSummary)
	currentCol = 1
	currentRow = 1
	maxItemsPerRow = 9
	firstRowSwipeDone = false
	finalLargeScanUsed = false
	statsLogged = false
	log.Info().Int("combinations", len(targetSkillCombinations)).Msg("<EssenceFilter> Step7 ok")
	log.Info().Msg("<EssenceFilter> ========== Init Done ==========")

	// 展示目标技能
	var skillIdSlots [3][]int
	for _, c := range targetSkillCombinations {
		for i, skillID := range c.SkillIDs {
			skillIdSlots[i] = append(skillIdSlots[i], skillID)
		}
	}

	var skillBuilder strings.Builder
	skillBuilder.WriteString(`<div style="color: #00bfff; font-weight: 900;">目标技能列表：</div>`)

	slotColors := []string{"#47b5ff", "#11dd11", "#e877fe"} // Placeholders for Slot 1, 2, 3

	for i, idSlot := range skillIdSlots {
		// Get unique skill names
		uniqueIds := make(map[int]struct{})
		for _, id := range idSlot {
			uniqueIds[id] = struct{}{}
		}

		// getPoolBySlot is defined in filter.go, skillNameByID is in loader.go
		pool := getPoolBySlot(i + 1)
		skillNames := make([]string, 0, len(uniqueIds))
		for id := range uniqueIds {
			skillNames = append(skillNames, skillNameByID(id, pool))
		}
		sort.Strings(skillNames)

		if len(skillNames) == 0 {
			continue
		}

		// Build table for the slot
		slotColor := slotColors[i]
		skillBuilder.WriteString(fmt.Sprintf(`<div style="color: %s; font-weight: 700;">词条 %d:</div>`, slotColor, i+1))

		const columns = 3
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

	return true
}

type OCREssenceInventoryNumberAction struct{}

func (a *OCREssenceInventoryNumberAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	const maxSinglePage = 45 // 单页可见格子上限：9列×5行

	if arg.RecognitionDetail == nil || arg.RecognitionDetail.Results == nil {
		log.Error().Msg("<EssenceFilter> CheckTotal: no OCR detail")
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
		log.Error().Msg("<EssenceFilter> CheckTotal: empty text")
		return false
	}

	// 提取数字：若是 “cur/total” 取 cur，否则取第一个数字
	re := regexp.MustCompile(`\d+`)
	nums := re.FindAllString(text, -1)
	if len(nums) == 0 {
		log.Error().Str("text", text).Msg("<EssenceFilter> CheckTotal: no number found")
		return false
	}
	nStr := nums[0] // 优先取 cur；若只有一个数字就取它
	n, err := strconv.Atoi(nStr)
	if err != nil {
		log.Error().Err(err).Str("text", text).Msg("<EssenceFilter> CheckTotal: parse fail")
		return false
	}

	log.Info().Int("count", n).Int("max_single_page", maxSinglePage).Str("raw", text).
		Msg("<EssenceFilter> CheckTotal: parsed")
	LogMXUSimpleHTML(
		ctx,
		fmt.Sprintf("库存中共 <span style=\"color: #ff7000; font-weight: 900;\">%d</span> 个基质", n),
	)

	if n <= maxSinglePage {
		ctx.OverrideNext(arg.CurrentTaskName, []maa.NextItem{
			{Name: "EssenceDetectFinal"},
		})
	}
	return true
}

// EssenceFilterCheckItemAction - OCR skills and match
type EssenceFilterCheckItemAction struct{}

func (a *EssenceFilterCheckItemAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	log.Info().Msg("<EssenceFilter> ---- CheckItem ----")

	if !statsLogged {
		logFilteredSkillStats()
		statsLogged = true
	}

	// parse slot info from custom_action_param: {"slot":1,"is_last":false}
	var params struct {
		Slot   int  `json:"slot"`
		IsLast bool `json:"is_last"`
	}
	if arg.CustomActionParam != "" {
		_ = json.Unmarshal([]byte(arg.CustomActionParam), &params)
	}
	if params.Slot < 1 || params.Slot > 3 {
		log.Error().Int("slot", params.Slot).Msg("<EssenceFilter> invalid slot param")
		return false
	}
	if params.Slot == 1 {
		currentSkills = [3]string{}
		currentSkillLevels = [3]int{}
	}

	if arg.RecognitionDetail == nil || arg.RecognitionDetail.Results == nil {
		log.Error().Msg("<EssenceFilter> OCR detail missing from pipeline")
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
		log.Error().Int("slot", params.Slot).Str("raw", rawText).Msg("<EssenceFilter> OCR empty")
		return false
	}
	currentSkills[params.Slot-1] = text
	log.Info().Int("slot", params.Slot).Str("skill", rawText).Bool("is_last", params.IsLast).Msg("<EssenceFilter> OCR ok")

	if !params.IsLast {
		// wait for next slot
		return true
	}

	// last slot: ensure all slots filled
	for i, s := range currentSkills {
		if s == "" {
			log.Error().Int("slot", i+1).Msg("<EssenceFilter> missing skill for slot")
			return false
		}
	}

	// Let SkillDecision action handle match/lock routing
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
		log.Error().Int("slot", params.Slot).Msg("<EssenceFilter> invalid level slot param")
		return false
	}

	if arg.RecognitionDetail == nil || arg.RecognitionDetail.Results == nil {
		log.Error().Int("slot", params.Slot).Msg("<EssenceFilter> level OCR detail missing")
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
		log.Error().Int("slot", params.Slot).Msg("<EssenceFilter> level OCR empty")
		return false
	}
	if m := levelParseRe.FindStringSubmatch(rawText); len(m) >= 2 {
		if lv, err := strconv.Atoi(m[1]); err == nil && lv >= 1 && lv <= 6 {
			currentSkillLevels[params.Slot-1] = lv
			log.Info().Int("slot", params.Slot).Int("level", lv).Str("raw", rawText).Msg("<EssenceFilter> OCR level ok")
			return true
		}
	}
	log.Error().Int("slot", params.Slot).Str("raw", rawText).Msg("<EssenceFilter> level parse fail")
	return false
}

// EssenceFilterRowCollectAction - collect boxes in a row (TemplateMatch detail) + ColorMatch filter, click first
type EssenceFilterRowCollectAction struct{}

func (a *EssenceFilterRowCollectAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if arg.RecognitionDetail == nil || arg.RecognitionDetail.Results == nil || arg.RecognitionDetail.Hit == false {
		log.Error().Msg("<EssenceFilter> RowCollect: 识别详情或结果为空")
		return false
	}

	// 优先使用 Filtered 结果，如果没有则回退到 All
	results := arg.RecognitionDetail.Results.Filtered
	if len(results) == 0 {
		results = arg.RecognitionDetail.Results.All
	}

	controller := ctx.GetTasker().GetController()
	if controller == nil {
		log.Error().Msg("<EssenceFilter> RowCollect: controller nil")
		return false
	}
	controller.PostScreencap().Wait()
	img, err := controller.CacheImage()
	if err != nil {
		log.Error().Err(err).Msg("<EssenceFilter> RowCollect: get screenshot failed")
		return false
	}

	rowBoxes = rowBoxes[:0]
	for _, res := range results {
		tm, ok := res.AsTemplateMatch()
		if !ok {
			continue
		}
		b := tm.Box
		boxArr := [4]int{b.X(), b.Y(), b.Width(), b.Height()}

		colorMatchROIX := boxArr[0]
		colorMatchROIY := boxArr[1] + 90
		colorMatchROIW := boxArr[2]
		colorMatchROIH := boxArr[3] - 90
		if colorMatchROIW <= 0 || colorMatchROIH <= 0 {
			log.Error().Ints("box", boxArr[:]).Msg("<EssenceFilter> RowCollect: invalid ROI size, skip")
			continue // skip invalid ROIs
		}

		roi := maa.Rect{colorMatchROIX, colorMatchROIY, colorMatchROIW, colorMatchROIH}

		for _, et := range EssenceTypes {
			ColorMatchOverrideParam := map[string]any{
				"EssenceColorMatch": map[string]any{
					"roi":   roi,
					"lower": et.Range.Lower,
					"upper": et.Range.Upper,
				},
			}
			cDetail, err := ctx.RunRecognition("EssenceColorMatch", img, ColorMatchOverrideParam)

			if err != nil {
				log.Error().Err(err).Ints("box", boxArr[:]).Msg("<EssenceFilter> RowCollect: ColorMatch failed")
				continue
			}

			if cDetail != nil && cDetail.Hit {
				rowBoxes = append(rowBoxes, boxArr)
				break
			}
		}
	}
	// sort rowboxes by Y coordinate then X coordinate
	sort.Slice(rowBoxes, func(i, j int) bool {
		if rowBoxes[i][1] == rowBoxes[j][1] {
			return rowBoxes[i][0] < rowBoxes[j][0]
		}
		return rowBoxes[i][1] < rowBoxes[j][1]
	})

	// LogMXUSimpleHTML(ctx, "len(results): "+strconv.Itoa(len(results))+", valid boxes after color match: "+strconv.Itoa(len(rowBoxes)))
	log.Info().Int("len_results", len(results)).Int("valid_boxes", len(rowBoxes)).Msg("<EssenceFilter> RowCollect: color match done")
	// 如果本行没有任何符合条件的box，且还没有使用过最终大范围扫描，则触发最终大范围扫描；否则直接结束当前行的处理
	isFallbackScan := arg.CurrentTaskName == "EssenceDetectFinal"

	if isFallbackScan && !finalLargeScanUsed {
		finalLargeScanUsed = true
		ctx.OverrideNext(arg.CurrentTaskName, []maa.NextItem{
			{Name: "EssenceDetectFinal"},
		})
		LogMXUSimpleHTMLWithColor(
			ctx,
			"尾扫完成，收集所有剩余基质格子",
			"#1a01fd",
		)
		log.Info().Msg("<EssenceFilter> RowCollect: trigger final large scan")
		return true
	}

	// 在非尾扫的情况下，如果符合条件的box数量超过单行最大可处理数量，直接结束当前行的处理，避免误操作；如果是尾扫，则不论数量多少都继续处理
	if (len(rowBoxes) > maxItemsPerRow) && !isFallbackScan {
		log.Error().Int("count", len(rowBoxes)).Msg("<EssenceFilter> RowCollect: boxes > maxItemsPerRow, abort")
		ctx.OverrideNext(arg.CurrentTaskName, []maa.NextItem{
			{Name: "EssenceFilterFinish"},
		})
		return true
	}
	if len(rowBoxes) == 0 {
		log.Info().Msg("<EssenceFilter> RowCollect: no valid boxes, finish")
		ctx.OverrideNext(arg.CurrentTaskName, []maa.NextItem{
			{Name: "EssenceFilterFinish"},
		})
		return true
	}

	rowIndex = 0
	ctx.OverrideNext(arg.CurrentTaskName, []maa.NextItem{
		{Name: "EssenceFilterRowNextItem"},
	})
	return true
}

// EssenceFilterRowNextItemAction - proceed to next box or swipe/finish
type EssenceFilterRowNextItemAction struct{}

func (a *EssenceFilterRowNextItemAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	// ensure we exit detail before next

	if rowIndex >= len(rowBoxes) {
		if (len(rowBoxes) == maxItemsPerRow) && !finalLargeScanUsed {
			var nextSwipe string
			if !firstRowSwipeDone {
				nextSwipe = "EssenceFilterSwipeFirst"
				firstRowSwipeDone = true
			} else {
				nextSwipe = "EssenceFilterSwipeNext"
			}

			LogMXUSimpleHTML(
				ctx,
				fmt.Sprintf("滑动到第 %d 行", currentRow+1),
			)
			currentRow++

			ctx.OverrideNext(arg.CurrentTaskName, []maa.NextItem{
				{Name: nextSwipe},
			})
			return true
		}
		ctx.OverrideNext(arg.CurrentTaskName, []maa.NextItem{
			{Name: "EssenceFilterFinish"},
		})
		return true
	}

	box := rowBoxes[rowIndex]
	cx := box[0] + box[2]/2
	cy := box[1] + box[3]/2
	log.Info().Ints("box", box[:]).Int("cx", cx).Int("cy", cy).Msg("<EssenceFilter> RowNextItem: click next box")

	clickingBox := [4]int{box[0] + 10, box[1] + 10, box[2] - 20, box[3] - 20} // click center with a small box
	ClickingBoxOverrideParam := map[string]any{
		"NodeClick": map[string]any{
			"action": map[string]any{
				"param": map[string]any{
					"target": clickingBox,
				},
			},
		},
	}
	ctx.RunTask("NodeClick", ClickingBoxOverrideParam)

	visitedCount++
	rowIndex++
	ctx.OverrideNext(arg.CurrentTaskName, []maa.NextItem{
		{Name: "EssenceFilterCheckItemSlot1"},
	})
	return true
}

// EssenceFilterSkillDecisionAction - match skills then decide lock or skip
type EssenceFilterSkillDecisionAction struct{}

func (a *EssenceFilterSkillDecisionAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	skills := []string{currentSkills[0], currentSkills[1], currentSkills[2]}
	opts, _ := getOptionsFromAttach(ctx, "EssenceFilterInit")
	if opts == nil {
		opts = &EssenceFilterOptions{}
	}

	// 优先：原始技能组合匹配
	matchResult, matched := MatchEssenceSkills(ctx, skills)

	// 次优先：保留未来可期基质、保留实用基质
	extendedReason := ""
	if !matched && opts != nil {
		if opts.KeepFuturePromising && opts.FuturePromisingMinTotal > 0 {
			if MatchFuturePromising(skills, currentSkillLevels, opts.FuturePromisingMinTotal) {
				matched = true
				sum := currentSkillLevels[0] + currentSkillLevels[1] + currentSkillLevels[2]
				matchResult = &SkillCombinationMatch{
					SkillIDs:      []int{0, 0, 0},
					SkillsChinese: []string{skills[0], skills[1], skills[2]},
					Weapons:       []WeaponData{},
				}
				extendedReason = fmt.Sprintf("未来可期：总等级 %d ≥ %d", sum, opts.FuturePromisingMinTotal)
				extFuturePromisingCount++
				log.Info().
					Strs("skills", skills).
					Ints("levels", currentSkillLevels[:]).
					Int("sum", sum).
					Int("min_total", opts.FuturePromisingMinTotal).
					Msg("<EssenceFilter> MatchFuturePromising: 保留未来可期基质")
			}
		}
		slot3MinLv := opts.Slot3MinLevel
		if slot3MinLv <= 0 {
			slot3MinLv = 3
		}
		if !matched && opts.KeepSlot3Level3Practical {
			var slot3Match bool
			matchResult, slot3Match = MatchSlot3Level3Practical(skills, currentSkillLevels, slot3MinLv)
			if slot3Match {
				matched = true
				extendedReason = fmt.Sprintf("实用基质：词条3(%s)等级 %d ≥ %d", skills[2], currentSkillLevels[2], slot3MinLv)
				extSlot3PracticalCount++
				log.Info().
					Str("slot3_skill", skills[2]).
					Int("slot3_level", currentSkillLevels[2]).
					Int("min_level", slot3MinLv).
					Msg("<EssenceFilter> MatchSlot3Level3Practical: 保留实用基质")
			}
		}
	}
	MatchedMessageColor := "#00bfff"
	if matched {
		MatchedMessageColor = "#064d7c"
	}

	LogMXUSimpleHTMLWithColor(
		ctx,
		fmt.Sprintf("OCR到技能：%s(+%d) | %s(+%d) | %s(+%d)",
			skills[0], currentSkillLevels[0],
			skills[1], currentSkillLevels[1],
			skills[2], currentSkillLevels[2]),
		MatchedMessageColor,
	)
	if matched && extendedReason != "" {
		// 扩展规则命中：无武器列表，独立处理
		matchedCount++
		log.Info().
			Strs("skills", skills).
			Str("reason", extendedReason).
			Int("matched_count", matchedCount).
			Msg("<EssenceFilter> extended rule hit, lock next")

		LogMXUHTML(ctx, fmt.Sprintf(
			`<div style="color: #064d7c; font-weight: 900;">🔒 扩展规则命中：%s</div>`,
			escapeHTML(extendedReason),
		))

		ctx.OverrideNext(arg.CurrentTaskName, []maa.NextItem{
			{Name: "EssenceFilterLockItemLog"},
		})
	} else if matched {
		// 武器匹配命中
		matchedCount++

		weaponNames := make([]string, 0, len(matchResult.Weapons))
		for _, w := range matchResult.Weapons {
			weaponNames = append(weaponNames, w.ChineseName)
		}

		log.Info().
			Strs("weapons", weaponNames).
			Strs("skills", skills).
			Ints("skill_ids", matchResult.SkillIDs).
			Int("matched_count", matchedCount).
			Msg("<EssenceFilter> match ok, lock next")

		var weaponsHTML strings.Builder
		for i, w := range matchResult.Weapons {
			if i > 0 {
				weaponsHTML.WriteString("、")
			}
			weaponColor := getColorForRarity(w.Rarity)
			weaponsHTML.WriteString(fmt.Sprintf(
				`<span style="color: %s;">%s</span>`,
				weaponColor, escapeHTML(w.ChineseName),
			))
		}
		LogMXUHTML(ctx, fmt.Sprintf(
			`<div style="color: #064d7c; font-weight: 900;">匹配到武器：%s</div>`,
			weaponsHTML.String(),
		))

		key := skillCombinationKey(matchResult.SkillIDs)
		if key != "" {
			if s, ok := matchedCombinationSummary[key]; ok {
				s.Count++
			} else {
				idsCopy := append([]int(nil), matchResult.SkillIDs...)
				cfgSkillsCopy := append([]string(nil), matchResult.SkillsChinese...)
				ocrSkillsCopy := append([]string(nil), skills...)
				weaponsCopy := make([]WeaponData, len(matchResult.Weapons))
				copy(weaponsCopy, matchResult.Weapons)
				matchedCombinationSummary[key] = &SkillCombinationSummary{
					SkillIDs:      idsCopy,
					SkillsChinese: cfgSkillsCopy,
					OCRSkills:     ocrSkillsCopy,
					Weapons:       weaponsCopy,
					Count:         1,
				}
			}
		}

		ctx.OverrideNext(arg.CurrentTaskName, []maa.NextItem{
			{Name: "EssenceFilterLockItemLog"},
		})
	} else {
		// 未匹配：根据选项决定是跳过还是废弃
		if opts.DiscardUnmatched {
			log.Info().Strs("skills", skills).Msg("<EssenceFilter> not matched, discard item")
			LogMXUHTML(ctx, `<div style="color: #ff6b6b; font-weight: 900;">🗑️ 未匹配到目标技能组合，废弃该物品</div>`)
			ctx.OverrideNext(arg.CurrentTaskName, []maa.NextItem{
				{Name: "EssenceFilterDiscardItemLog"},
			})
		} else {
			log.Info().Strs("skills", skills).Msg("<EssenceFilter> not matched, skip to next item")
			LogMXUSimpleHTML(ctx, "未匹配到目标技能组合，跳过该物品")
			ctx.OverrideNext(arg.CurrentTaskName, []maa.NextItem{
				{Name: "EssenceFilterRowNextItem"},
			})
		}
	}

	currentSkills = [3]string{}
	currentSkillLevels = [3]int{}
	return true
}

// EssenceFilterFinishAction - finish and reset
type EssenceFilterFinishAction struct{}

func (a *EssenceFilterFinishAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	log.Info().Msg("<EssenceFilter> ========== Finish ==========")
	log.Info().Int("matched_total", matchedCount).Msg("<EssenceFilter> locked items")

	LogMXUSimpleHTMLWithColor(
		ctx,
		fmt.Sprintf("筛选完成！共历遍物品：%d，确认锁定物品：%d", visitedCount, matchedCount),
		"#11cf00",
	)

	// 追加本轮战利品摘要
	logMatchSummary(ctx)

	// 扩展规则统计
	opts, _ := getOptionsFromAttach(ctx, "EssenceFilterInit")
	if opts != nil {
		if opts.KeepFuturePromising {
			LogMXUSimpleHTMLWithColor(ctx,
				fmt.Sprintf("扩展规则「未来可期」锁定：%d 个", extFuturePromisingCount),
				"#064d7c",
			)
		}
		if opts.KeepSlot3Level3Practical {
			LogMXUSimpleHTMLWithColor(ctx,
				fmt.Sprintf("扩展规则「实用基质」锁定：%d 个", extSlot3PracticalCount),
				"#064d7c",
			)
		}
	}

	targetSkillCombinations = nil
	matchedCount = 0
	visitedCount = 0
	extFuturePromisingCount = 0
	extSlot3PracticalCount = 0
	for i := range filteredSkillStats {
		filteredSkillStats[i] = nil
	}
	matchedCombinationSummary = nil
	statsLogged = false
	currentCol = 1
	currentRow = 1
	finalLargeScanUsed = false
	firstRowSwipeDone = false
	rowBoxes = nil
	rowIndex = 0

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
	log.Info().Str("step", params.Step).Str("node", arg.CurrentTaskName).Msg("<EssenceFilter> Trace")
	return true
}
