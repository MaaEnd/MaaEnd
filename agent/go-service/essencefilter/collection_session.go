package essencefilter

import (
	"fmt"
	"sort"
	"strings"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/essencefilter/matchapi"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

// 840 全收集模式的编排层。
//
// 两个 Pass 各是一次独立的 MaaTask（由 Pipeline 的 SubTask 触发），因此每次进入
// EssenceFilterInit 都会重建 currentRun，但不会清掉这里的会话变量：
//
//	Pass 1 (phase=scan)  : 只读盘点 -> currentCollection
//	Plan 节点            : 算配额   -> currentCollectionPlan，并把 phase 改成 apply
//	Pass 2 (phase=apply) : 按配额锁定/丢弃，消耗 currentCollectionPlan
//	Report 节点          : 输出 MXU 与网页板块，然后清空全部会话变量
//
// 因为 Pipeline 里走的是 SubTask（ctx.RunTask），两个 Pass 各自拿到新的 TaskId，
// C++ 的 EssenceGrid 网格追踪器才会 reset；在同一个 TaskId 里跑两遍是不会 reset 的。

// nodeCollectionReport 是汇报节点名，预演时由 Plan 动作直接接上。
const nodeCollectionReport = "EssenceFilterCollectionReport"

// collectionPhase 标记一次 EssenceFilter 运行属于收集模式的哪个阶段。
type collectionPhase int

const (
	collectionPhaseNone  collectionPhase = iota
	collectionPhaseScan                  // 只读盘点，不修改任何标记
	collectionPhaseApply                 // 按配额执行锁定/丢弃
)

// parseCollectionPhase 解析 attach 里的阶段标记；未知值返回 none。
func parseCollectionPhase(v string) collectionPhase {
	switch v {
	case "scan":
		return collectionPhaseScan
	case "apply":
		return collectionPhaseApply
	default:
		return collectionPhaseNone
	}
}

// 会话变量：跨两个 Pass 的 SubTask 边界存活。
var (
	currentCollection              *collectionState
	currentCollectionPlan          *collectionPlan
	currentCollectionMode          collectionKeepMode
	currentCollectionEngine        *matchapi.Engine
	currentCollectionDryRun        bool
	currentCollectionDiscardLocked bool
	currentCollectionLockKeepers   bool
)

// clearCollectionSession 释放会话状态。正常路径由 Report 节点调用；
// 异常中断时下一次盘点遍的 Init 会直接覆盖，因此不会串用旧数据。
func clearCollectionSession() {
	currentCollection = nil
	currentCollectionPlan = nil
	currentCollectionEngine = nil
	currentCollectionDryRun = false
	currentCollectionDiscardLocked = false
	currentCollectionLockKeepers = false
}

// collectionSkillNames 把槽位 id 映射为当前语言下的词条名，用于网页与日志展示。
type collectionSkillNames struct {
	bySlot [3]map[int]string
}

func newCollectionSkillNames(pools matchapi.SkillPools) *collectionSkillNames {
	names := &collectionSkillNames{}
	build := func(list []matchapi.SkillPool) map[int]string {
		m := make(map[int]string, len(list))
		for _, entry := range list {
			m[entry.ID] = entry.Chinese
		}
		return m
	}
	names.bySlot[0] = build(pools.Slot1)
	names.bySlot[1] = build(pools.Slot2)
	names.bySlot[2] = build(pools.Slot3)
	return names
}

// text 渲染一个组合，例如「力量 | 攻击 | 强攻」。
func (n *collectionSkillNames) text(combo collectionCombo) string {
	parts := make([]string, 0, len(combo))
	for slot, id := range combo {
		name, ok := n.bySlot[slot][id]
		if !ok {
			name = fmt.Sprintf("#%d", id)
		}
		parts = append(parts, name)
	}
	return strings.Join(parts, " | ")
}

// collectionMissingItem 是网页与日志里的一条「建议刷取组合」。
type collectionMissingItem struct {
	ComboText string
	Scarcity  int
	Locations []string
}

// collectionLocationPlan 是按地点聚合的刷取建议：这个点能补哪些缺口。
//
// 刷取决策天然是「先去哪个点」，所以主视图按地点组织，而不是把 840 个组合平铺出来。
type collectionLocationPlan struct {
	Name string
	// ShortName 去掉「重度能量淤积点·」这类公共前缀（取最后一个「·」之后的部分），
	// 12 个地点并列时前缀纯属噪音。
	ShortName string
	// Combos 是该地点能产出、而本次盘点仍缺失的组合文本（按组合升序）。
	Combos []string
	// MissingCount 等于 len(Combos)，模板里直接可用。
	MissingCount int
	// ScarceCount 是这些缺口里「可选地点数最少」的那一档的数量，用于提示先解决难凑的。
	ScarceCount int
	// MinScarcity 是全局最小稀缺度（最少几个地点能出）。
	MinScarcity int
	// Slot2Text / Slot3Text 是该地点的词条池（附加 / 技能）拼好的展示串，
	// 说明这个点能刷的范围。在 Go 侧拼好，模板里就不用嵌套 range 了。
	Slot2Text string
	Slot3Text string
	// PreviewText 是卡片里实际展示的组合串（按稀缺度优先、最多 collectionCombosPerCard 条），
	// MoreCount 是被截掉的条数。有界是刻意的：日志与网页共用同一份 HTML，
	// 内容必须短到日志里也能看（与原版 plan_card 每地点最多 2 张卡同一思路）。
	PreviewText string
	MoreCount   int
}

// collectionCombosPerCard 是每个地点卡片最多列出的组合数。
const collectionCombosPerCard = 8

// collectionBoard 是 840 全收集板块的展示数据。
type collectionBoard struct {
	Collected int
	Total     int
	Percent   string
	KeepMode  collectionKeepMode
	Missing   []collectionMissingItem
	// DryRun 为真时 Locked / Spared / Discarded 是计划值，而非实际计数。
	DryRun bool
	Locked int
	// Spared 是命中保留配额但未新增锁定（关闭「未锁定的进行锁定」）的份数。
	Spared    int
	Discarded int
	Skipped   int

	// Locations 是按「可补缺口数」降序排列的刷取建议（网页完整清单）。
	Locations []collectionLocationPlan
	// TopLocations 是同一列表的前若干条，供 MXU 日志里的精简摘要使用。
	TopLocations []collectionLocationPlan
	// MissingCount / ScarceTotal 是全局面板上的汇总数字。
	MissingCount int
	ScarceTotal  int
	// Scanned 是本次盘点到多少个无瑕（金色）基质，Signatures 是其中不同的等级签名数。
	//
	// 这三个数以前只在日志里以 classes / signatures / will_keep+will_discard 的形式
	// 间接出现，读者很容易把「类」当成「我有几个基质」。现在显式报出来。
	Scanned    int
	Signatures int
}

// buildCollectionBoard 汇总盘点结果与执行统计。plan 允许为 nil（例如盘点后中断）。
func buildCollectionBoard(
	state *collectionState,
	plan *collectionPlan,
	mode collectionKeepMode,
	engine *matchapi.Engine,
	dryRun bool,
) *collectionBoard {
	board := &collectionBoard{KeepMode: mode, DryRun: dryRun}
	if engine == nil {
		return board
	}
	pools := engine.SkillPools()
	locations := engine.Locations()
	universe := collectionUniverse(pools)
	board.Total = len(universe)
	var missing []collectionCombo
	if state != nil {
		board.Collected = state.classCount()
		board.Scanned = state.scanned
		missing = state.missing(universe)
		sortCollectionMissing(missing, locations)
		names := newCollectionSkillNames(pools)
		board.Missing = make([]collectionMissingItem, 0, len(missing))
		for _, combo := range missing {
			board.Missing = append(board.Missing, collectionMissingItem{
				ComboText: names.text(combo),
				Scarcity:  collectionScarcity(locations, combo),
				Locations: collectionLocationsFor(locations, combo),
			})
		}
	}
	if plan != nil {
		board.Signatures = len(plan.quota)
		if dryRun {
			// 预演没有执行遍，用计划值代替实际计数。
			if plan.lockKeepers {
				board.Locked = plan.keepTotal()
			} else {
				board.Spared = plan.keepTotal()
			}
			if state != nil {
				board.Discarded = state.scanned - plan.keepTotal()
			}
			board.Skipped = 0
		} else {
			board.Locked = plan.locked
			board.Spared = plan.spared
			board.Discarded = plan.discarded
			board.Skipped = plan.skipped
		}
	}
	if board.Total > 0 {
		board.Percent = fmt.Sprintf("%.1f", float64(board.Collected)*100/float64(board.Total))
	} else {
		board.Percent = "0.0"
	}
	board.MissingCount = len(missing)
	board.Locations = buildLocationPlans(state, engine, universe, missing)
	const topLocationsInLog = 6
	if len(board.Locations) > topLocationsInLog {
		board.TopLocations = board.Locations[:topLocationsInLog]
	} else {
		board.TopLocations = board.Locations
	}
	for _, plan := range board.Locations {
		board.ScarceTotal += plan.ScarceCount
	}
	return board
}

// shortLocationName 取地点名最后一个分隔符之后的部分；没有分隔符时原样返回。
func shortLocationName(name string) string {
	if idx := strings.LastIndex(name, "·"); idx >= 0 && idx+len("·") < len(name) {
		return name[idx+len("·"):]
	}
	return name
}

// buildLocationPlans 把缺失组合按「哪个地点能产出」重新聚合，并按可补数量降序排列。
func buildLocationPlans(
	state *collectionState,
	engine *matchapi.Engine,
	universe []collectionCombo,
	missing []collectionCombo,
) []collectionLocationPlan {
	if engine == nil || len(missing) == 0 {
		return nil
	}
	locations := engine.Locations()
	names := newCollectionSkillNames(engine.SkillPools())

	minScarcity := 0
	for i, combo := range missing {
		n := collectionScarcity(locations, combo)
		if i == 0 || n < minScarcity {
			minScarcity = n
		}
	}

	// 组合文本按组合升序，保证同一地点内的输出稳定。
	sorted := append([]collectionCombo(nil), missing...)
	sort.Slice(sorted, func(i, j int) bool { return comboLess(sorted[i], sorted[j]) })

	slot2Name := make(map[int]string, len(engine.SkillPools().Slot2))
	for _, e := range engine.SkillPools().Slot2 {
		slot2Name[e.ID] = e.Chinese
	}
	slot3Name := make(map[int]string, len(engine.SkillPools().Slot3))
	for _, e := range engine.SkillPools().Slot3 {
		slot3Name[e.ID] = e.Chinese
	}

	plans := make([]collectionLocationPlan, 0, len(locations))
	for _, loc := range locations {
		plan := collectionLocationPlan{
			Name:        loc.Name,
			ShortName:   shortLocationName(loc.Name),
			MinScarcity: minScarcity,
		}
		var slot2, slot3 []string
		for _, id := range loc.Slot2IDs {
			if name, ok := slot2Name[id]; ok {
				slot2 = append(slot2, name)
			}
		}
		for _, id := range loc.Slot3IDs {
			if name, ok := slot3Name[id]; ok {
				slot3 = append(slot3, name)
			}
		}
		plan.Slot2Text = strings.Join(slot2, i18n.Separator())
		plan.Slot3Text = strings.Join(slot3, i18n.Separator())
		for _, combo := range sorted {
			if !collectionLocationHas(loc, combo) {
				continue
			}
			plan.Combos = append(plan.Combos, names.text(combo))
			if collectionScarcity(locations, combo) == minScarcity {
				plan.ScarceCount++
			}
		}
		plan.MissingCount = len(plan.Combos)

		// 卡片内容有界：先按稀缺度升序（只在这里能出的最该先解决），再按组合序取前 N 条。
		preview := append([]collectionCombo(nil), sorted...)
		sort.SliceStable(preview, func(i, j int) bool {
			si, sj := collectionScarcity(locations, preview[i]), collectionScarcity(locations, preview[j])
			if si != sj {
				return si < sj
			}
			return comboLess(preview[i], preview[j])
		})
		shown := make([]string, 0, collectionCombosPerCard)
		for _, combo := range preview {
			if !collectionLocationHas(loc, combo) {
				continue
			}
			if len(shown) >= collectionCombosPerCard {
				plan.MoreCount++
				continue
			}
			shown = append(shown, names.text(combo))
		}
		plan.PreviewText = strings.Join(shown, i18n.Separator())
		if plan.MissingCount > 0 {
			plans = append(plans, plan)
		}
	}
	sort.SliceStable(plans, func(i, j int) bool { return plans[i].MissingCount > plans[j].MissingCount })
	return plans
}

// --- Plan 节点 ---

// EssenceFilterCollectionPlanAction 在盘点遍结束后计算保留配额，并把阶段标记改成 apply，
// 供随后的执行遍 SubTask 读取。
type EssenceFilterCollectionPlanAction struct{}

var _ maa.CustomActionRunner = &EssenceFilterCollectionPlanAction{}

func (a *EssenceFilterCollectionPlanAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if currentCollection == nil {
		reportFocusByKey(ctx, "focus.error.collection_no_plan")
		return false
	}
	mode := currentCollectionMode
	plan := buildCollectionPlanWithPolicy(currentCollection, mode, currentCollectionLockKeepers)
	currentCollectionPlan = plan

	stats := currentCollection.classCount()
	log.Info().
		Str("component", "EssenceCollection").
		Str("step", "Plan").
		Str("keep_mode", mode.String()).
		Int("scanned_gold_essences", currentCollection.scanned).
		Int("classes", stats).
		Int("signatures", len(plan.quota)).
		Int("keep_total", plan.keepTotal()).
		Msg("collection plan built")

	if currentCollectionDryRun {
		// 预演：不进入执行遍，直接把汇报节点接上，全程不修改任何标记。
		log.Info().
			Str("component", "EssenceCollection").
			Str("step", "Plan").
			Bool("dry_run", true).
			Bool("lock_keepers", plan.lockKeepers).
			Int("will_keep", plan.keepTotal()).
			Int("will_discard", currentCollection.scanned-plan.keepTotal()).
			Msg("collection rehearsal plan built, no mark will change")
		if err := ctx.OverrideNext(arg.CurrentTaskName, []maa.NextItem{{Name: nodeCollectionReport}}); err != nil {
			log.Error().Err(err).Str("component", "EssenceCollection").Msg("OverrideNext failed")
			return false
		}
		return true
	}

	if err := ctx.OverridePipeline(map[string]any{
		"EssenceFilterInit": map[string]any{
			"attach": map[string]any{"collection_phase": "apply"},
		},
	}); err != nil {
		log.Error().Err(err).Str("component", "EssenceCollection").Msg("OverridePipeline failed")
		return false
	}
	return true
}

// --- Report 节点 ---

// EssenceFilterCollectionReportAction 输出 840 全收集板块（MXU + 追加网页），并清空会话状态。
type EssenceFilterCollectionReportAction struct{}

var _ maa.CustomActionRunner = &EssenceFilterCollectionReportAction{}

func (a *EssenceFilterCollectionReportAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	board := buildCollectionBoard(
		currentCollection, currentCollectionPlan,
		currentCollectionMode, currentCollectionEngine, currentCollectionDryRun,
	)
	ok := reportCollectionBoard(ctx, board)
	clearCollectionSession()
	return ok
}

// collectionScanFailed 报告盘点遍的致命读取错误。
// 盘点必须完整：任何一格读不出来都中止，避免用不完整的盘点结果去决定丢弃。
func collectionScanFailed(err error) bool {
	log.Error().Err(err).Str("component", "EssenceCollection").Str("step", "Scan").Msg("collection scan failed")
	return false
}

// overrideCollectionNext 把当前决策节点路由到共用的锁定/丢弃/继续节点。
func overrideCollectionNext(ctx *maa.Context, arg *maa.CustomActionArg, next string) bool {
	if err := ctx.OverrideNext(arg.CurrentTaskName, []maa.NextItem{{Name: next}}); err != nil {
		log.Error().Err(err).
			Str("component", "EssenceCollection").
			Str("next", next).
			Msg("OverrideNext failed")
		return false
	}
	return true
}
