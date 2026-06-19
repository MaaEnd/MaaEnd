package trialofswordmancy

import (
	"encoding/json"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/trialofswordmancy/solver"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/maafocus"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

// —— Decide 动作 ——

var _ maa.CustomActionRunner = &DecideAction{}

// DecideAction 反序列化 recognition 产出的 GameState，调 solver.Decide 取最优单步决策，
// 按决策用 OverrideNext 路由到执行节点。
//
// 无状态：不记忆任何进度。每步的完整 State 都由 recognition 从当前截图读出后传入；
// 本动作只做「求解 → 路由」，不对包级状态产生副作用。单步循环靠 pipeline 的 next
// 回到 TrialOfSwordmancyDecide（recognition 重新读图），直到 RemainCalc==0 / 奖励耗尽。
// solver 只返回单步最优决策（迁移文档 §7.5）。
type DecideAction struct{}

// Run 执行决策。
func (a *DecideAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if arg == nil {
		log.Error().Str("component", component).Msg("custom action arg is nil")
		return false
	}
	if arg.RecognitionDetail == nil {
		log.Error().Str("component", component).Msg("recognition detail is nil")
		return false
	}

	detailJSON := unwrapCustomDetail(arg.RecognitionDetail)
	if detailJSON == "" {
		log.Error().Str("component", component).Msg("recognition detail json is empty")
		return false
	}

	var gs GameState
	if err := json.Unmarshal([]byte(detailJSON), &gs); err != nil {
		log.Error().Err(err).Str("component", component).Msg("failed to parse game state")
		return false
	}

	// 配置以节点参数为权威来源（与 recognition 一致），覆盖 detail 内的可能滞后值。
	cfg, err := loadConfigFromNode(ctx, arg.CurrentTaskName)
	if err != nil {
		log.Warn().Err(err).Str("component", component).Msg("failed to load config, use detail config")
		cfg = gs.Config
	}

	slv := solverFor(cfg)
	outcomes := slv.Decide(gs.State)

	var best solver.Action
	for _, o := range outcomes {
		if o.IsBest {
			best = o.Action
			break
		}
	}

	// 不可达状态（手牌超牌库 / 违反过滤 / RemainCalc==0）：没有最优决策。
	if outcomes == nil || best == solver.ActionNone {
		log.Error().
			Str("component", component).
			Ints("hand", gs.State.Hand[:]).
			Int("remainCalc", gs.State.RemainCalc).
			Int("remainAband", gs.State.RemainAband).
			Int("remainDouble", gs.State.RemainDouble).
			Bool("isDoubled", gs.State.IsDoubled).
			Msg("no reachable MDP decision for recognized state; routing to finish")
		maafocus.Print(ctx, "选剑演武：当前局面无法决策（识别异常或奖励已耗尽），结束任务")
		if err := routeNext(ctx, arg.CurrentTaskName, nodeFinish); err != nil {
			log.Error().Err(err).Str("component", component).Msg("failed to route to finish")
			return false
		}
		return true
	}

	// 无状态：不推进任何跟踪。Remain*/IsDoubled/Hand 全部由下一步的 recognition 从截图重读。

	if err := routeNext(ctx, arg.CurrentTaskName, executeNode(best)); err != nil {
		log.Error().Err(err).Str("component", component).Str("action", best.String()).Msg("failed to override next")
		return false
	}

	log.Info().
		Str("component", component).
		Str("action", best.String()).
		Int("remainCalc", gs.State.RemainCalc).
		Int("remainAband", gs.State.RemainAband).
		Int("remainDouble", gs.State.RemainDouble).
		Bool("isDoubled", gs.State.IsDoubled).
		Ints("hand", gs.State.Hand[:]).
		Str("overflowMode", cfg.OverflowMode.String()).
		Msg("decision made")
	maafocus.Print(ctx, "选剑演武："+actionFocusLabel(best))

	return true
}

// executeNode 把最优决策映射到执行节点（迁移文档 §7.4）。
func executeNode(action solver.Action) string {
	switch action {
	case solver.DrawCard:
		return nodeDoDrawCard
	case solver.Double:
		return nodeDoDoubleReward
	case solver.Abandon:
		return nodeGiveUp
	case solver.Calculate:
		return nodeStartTrial
	default:
		return nodeFinish
	}
}

// routeNext 设置当前节点的 next 为单一执行节点。
func routeNext(ctx *maa.Context, currentNode, target string) error {
	return ctx.OverrideNext(currentNode, []maa.NextItem{{Name: target}})
}

// actionFocusLabel 返回决策的中文 UI 标签。
func actionFocusLabel(action solver.Action) string {
	switch action {
	case solver.DrawCard:
		return "抽牌"
	case solver.Abandon:
		return "放弃本局"
	case solver.Calculate:
		return "开始演算"
	case solver.Double:
		return "选择翻倍"
	default:
		return "未知决策"
	}
}

// —— 辅助：Custom 识别 detail 解包 ——

// unwrapCustomDetail 从 Custom 识别的 RecognitionDetail.DetailJson 中取出我们写入的
// 明文 JSON。Custom 识别结果在框架里可能被包成 {"best":{"detail": <raw>}}，兼容两种形态。
// 仿 autostockpile extractCustomRecognitionDetailJSON。
func unwrapCustomDetail(detail *maa.RecognitionDetail) string {
	if detail == nil || detail.DetailJson == "" {
		return ""
	}
	var wrapped struct {
		Best struct {
			Detail json.RawMessage `json:"detail"`
		} `json:"best"`
	}
	if err := json.Unmarshal([]byte(detail.DetailJson), &wrapped); err == nil && len(wrapped.Best.Detail) > 0 {
		return rawJSONToString(wrapped.Best.Detail)
	}
	return detail.DetailJson
}

func rawJSONToString(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	if raw[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return string(raw)
		}
		return s
	}
	return string(raw)
}
