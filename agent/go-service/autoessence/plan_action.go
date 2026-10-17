package autoessence

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/essencefilter/matchapi"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/maafocus"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const (
	componentPlan          = "AutoEssenceTargetPlan"
	essenceFilterData      = "data/EssenceFilter"
	nodeOnComplete         = "AutoEssenceOnComplete"
	nodeDispatcher         = "AutoEssenceDispatcher"
	nodeLootReportJumpBack = "[JumpBack]AutoEssenceAfterBattleRunEndLootReport"
	// nodeForceTeleportOnce 是「整次任务只传送一次」的守卫节点：max_hit=1，计数只在顶层任务开始时归零。
	// 目标模式换淤积点后由本动作清零它的计数，重新武装「先传送到新点最近锚点」这条到达流程；
	// 传送之后有没有走到、走没走通，仍然由该节点下游的地区导航链自己判断。
	nodeForceTeleportOnce = "__AutoEssenceForceTeleportOnce"
)

// planLocation 是一次选点结果对应的刷取点。
type planLocation struct {
	Key  string
	Name string
}

// lastPlanLocation 记录目标模式上一轮选中的刷取点，用于判断是否换了点、要不要重新武装传送。
var lastPlanLocation planLocation

var _ maa.CustomActionRunner = &TargetPlanAction{}

// planLocationChanged 判断本轮方案是否换了刷取点（首次选点不算换点），并回传上一轮的刷取点。
// 换点后的落库由 rememberPlanLocation 完成。
func planLocationChanged(plan *FarmPlan) (changed bool, prev planLocation) {
	prev = lastPlanLocation
	return prev.Key != "" && prev.Key != plan.LocationKey, prev
}

// rememberPlanLocation 记录本轮刷取点，供下一轮判断是否换点。
func rememberPlanLocation(plan *FarmPlan) {
	lastPlanLocation = planLocation{Key: plan.LocationKey, Name: plan.LocationName}
}

// regionLabel 取地区名的界面文案（$global.region.*）。
// 没配到映射、或文案缺失（i18n.T 会原样返回 key）时退回刷取点全名，保证提示不丢内容。
func regionLabel(locationKey, fallback string) string {
	labelKey, ok := RegionLabelKey(locationKey)
	if !ok {
		return fallback
	}
	label := i18n.T(labelKey)
	if label == labelKey {
		return fallback
	}
	return label
}

// TargetPlanAction is A1 for Target mode: compute missing weapons, pick best farm plan, override pipeline.
type TargetPlanAction struct{}

func (a *TargetPlanAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if arg == nil {
		log.Error().Str("component", componentPlan).Msg("got nil custom action arg")
		return false
	}

	selected, err := readSelectedWeapons(ctx, arg.CurrentTaskName)
	if err != nil {
		log.Error().Err(err).Str("component", componentPlan).Msg("failed to read attach")
		return false
	}
	if len(selected) == 0 {
		log.Error().Str("component", componentPlan).Msg("no weapons selected in attach")
		maafocus.Print(ctx, "🎯目标模式：未勾选任何武器，结束任务")
		return overrideNextFinish(ctx, arg.CurrentTaskName)
	}

	missing := make([]string, 0, len(selected))
	for _, id := range selected {
		if Count([]string{id}) < 1 {
			missing = append(missing, id)
		}
	}
	if len(missing) == 0 {
		log.Info().Str("component", componentPlan).Int("selected", len(selected)).Msg("all targets satisfied")
		maafocus.Print(ctx, "🎯目标模式：勾选武器的基质均已满足，结束任务")
		return overrideNextFinish(ctx, arg.CurrentTaskName)
	}

	if err := loadSkillExpected(essenceFilterData); err != nil {
		log.Error().Err(err).Str("component", componentPlan).Msg("failed to load skill expected names")
		return false
	}

	engine, err := matchapi.NewEngineFromDir(essenceFilterData)
	if err != nil {
		log.Error().Err(err).Str("component", componentPlan).Msg("failed to load match engine")
		return false
	}

	plan, err := SelectBestPlan(engine, missing)
	if err != nil {
		log.Error().Err(err).Str("component", componentPlan).Msg("failed to select farm plan")
		return false
	}
	if plan == nil {
		log.Error().Str("component", componentPlan).Strs("missing", missing).Msg("no feasible farm plan")
		maafocus.Print(ctx, "🎯目标模式：找不到可覆盖缺口武器的刷取方案，结束任务")
		return overrideNextFinish(ctx, arg.CurrentTaskName)
	}

	engravePatch, err := buildEngravePipelineOverride(plan)
	if err != nil {
		log.Error().Err(err).Str("component", componentPlan).Msg("failed to build engrave override")
		return false
	}

	// 换了淤积点：清零「整次任务只传送一次」的计数，让分发器先把人送到新点的最近锚点。
	// 否则人还站在旧点，会直接认到旧点的「激发」并用新方案的预刻写条件在旧点开战。
	// 这里只负责重新武装，传送完走没走到由地区导航链判断。
	changed, prev := planLocationChanged(plan)
	if changed {
		if err := ctx.ClearHitCount(nodeForceTeleportOnce); err != nil {
			log.Error().
				Err(err).
				Str("component", componentPlan).
				Str("node", nodeForceTeleportOnce).
				Msg("failed to re-arm force teleport after location change")
			return false
		}
		log.Info().
			Str("component", componentPlan).
			Str("from", prev.Key).
			Str("to", plan.LocationKey).
			Msg("farm location changed, force teleport re-armed")
	}
	rememberPlanLocation(plan)

	anchor := SetAnchorNode(plan.LocationKey)
	engravePatch[anchor] = map[string]any{
		"enabled": true,
		"next": []string{
			nodeDispatcher,
		},
	}

	if err := ctx.OverridePipeline(engravePatch); err != nil {
		log.Error().Err(err).Str("component", componentPlan).Msg("OverridePipeline failed")
		return false
	}

	needNames := make([]string, 0, len(plan.Needs))
	for _, w := range plan.Needs {
		needNames = append(needNames, w.ChineseName)
	}
	maafocus.Print(ctx, fmt.Sprintf(
		"🎯目标模式：缺口 %d → 前往 %s｜基础 %s+%s+%s｜固定%s %s｜本轮可覆盖 %d",
		len(missing),
		plan.LocationName,
		plan.Slot1Names[0], plan.Slot1Names[1], plan.Slot1Names[2],
		fixedSlotLabel(plan.FixedSlot), plan.FixedName,
		len(plan.Needs),
	))
	if changed {
		maafocus.Print(ctx, i18n.T(
			"task.AutoEssence.focus.location_changed",
			regionLabel(prev.Key, prev.Name),
			regionLabel(plan.LocationKey, plan.LocationName),
		))
	}
	log.Info().
		Str("component", componentPlan).
		Str("location", plan.LocationKey).
		Str("anchor", anchor).
		Ints("slot1", plan.Slot1IDs[:]).
		Int("fixed_slot", plan.FixedSlot).
		Int("fixed_id", plan.FixedID).
		Int("needs", len(plan.Needs)).
		Strs("need_weapons", needNames).
		Msg("farm plan selected")

	return overrideNext(ctx, arg.CurrentTaskName, anchor)
}

func fixedSlotLabel(slot int) string {
	switch slot {
	case 2:
		return "附加"
	case 3:
		return "技能"
	default:
		return fmt.Sprintf("slot%d", slot)
	}
}

func overrideNext(ctx *maa.Context, current, next string) bool {
	if err := ctx.OverrideNext(current, []maa.NextItem{{Name: next}}); err != nil {
		log.Error().Err(err).Str("component", componentPlan).Str("next", next).Msg("OverrideNext failed")
		return false
	}
	return true
}

// finishNextItems runs enabled AfterBattle loot report (JumpBack) then OnComplete,
// matching Target-mode OnSuccess chaining so EssenceFilterFinishAction still fires.
func finishNextItems() []maa.NextItem {
	return []maa.NextItem{
		{Name: nodeLootReportJumpBack},
		{Name: nodeOnComplete},
	}
}

func overrideNextFinish(ctx *maa.Context, current string) bool {
	if err := ctx.OverrideNext(current, finishNextItems()); err != nil {
		log.Error().Err(err).Str("component", componentPlan).Msg("OverrideNext finish path failed")
		return false
	}
	return true
}

func readSelectedWeapons(ctx *maa.Context, nodeName string) ([]string, error) {
	raw, err := ctx.GetNodeJSON(nodeName)
	if err != nil {
		return nil, err
	}
	var wrapper struct {
		Attach map[string]json.RawMessage `json:"attach"`
	}
	if err := json.Unmarshal([]byte(raw), &wrapper); err != nil {
		return nil, err
	}
	out := make([]string, 0)
	for key, val := range wrapper.Attach {
		if !strings.HasPrefix(key, "wpn_") {
			continue
		}
		var on bool
		if err := json.Unmarshal(val, &on); err != nil || !on {
			continue
		}
		out = append(out, key)
	}
	return out, nil
}
