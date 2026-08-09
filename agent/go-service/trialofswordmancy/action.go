package trialofswordmancy

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/maafocus"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/trialofswordmancy/solver"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

// —— Decide 动作 ——

var _ maa.CustomActionRunner = &DecideAction{}

// DecideAction 反序列化 recognition 产出的 GameState，调 solver.Decide 取最优单步决策，
// 按决策用 OverrideNext 路由到执行节点。求解器来自 RecognizeDeck 的异步预热
// （presolve.go awaitPreSolve），本动作只做「取结果 → 查表 → 路由」。
//
// 本身不持有状态：每步的完整 State 都由 recognition 读出后传入，本动作只做「取结果 → 路由」；
// 唯一的状态副作用是经 roundState.OnRoundEnd 落定轮次边界（放弃递减、牌库重置，见 roundstate.go），
// 具体规则（跨日残局白送、0 不减）在那边，这里不重复。
// 单步循环靠 pipeline 的 next 回到 TrialOfSwordmancyDecide（recognition 重新读图），
// 直到奖励耗尽（pipeline 检测 → Finish）。solver 只返回单步最优决策。
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

	// 配置：牌库/手牌/剩余次数/翻倍态来自 recognition 截图识别；溢出模式是玩家策略选项，
	// 唯一数据源是 Decide 节点的 custom_action_param，与 RecognizeDeck 预求解同源同值
	// （decideOverflowMode）。读不到 = 配置错误，硬中止。
	overflowMode, ok := decideOverflowMode(ctx)
	if !ok {
		log.Error().Str("component", component).Msg("decide overflowMode missing or invalid, aborting")
		return false
	}
	cfg := gs.Config
	cfg.OverflowMode = overflowMode

	// 取用本轮异步预求解的求解器（presolve.go awaitPreSolve）；失败 = 系统性 bug，中止。
	slv, ok := awaitPreSolve(cfg)
	if !ok {
		return false
	}
	outcomes := slv.Decide(gs.State)

	// 不直接用求解器 Policy（其并列时按 [DrawCard,Abandon,Calculate] 取首位=抽牌）；
	// 复刻 TS 计算器（trial-of-swordmancy-strategy.vue）的推荐规则：抽牌与放弃总价值差 <1 时优先放弃。
	best := pickDecision(outcomes)

	// 不可达：识别产出了不在 MDP 状态空间的局面（ROI/模板未校准、读错、手牌超牌库等），是错误而非
	// 「奖励耗尽」（耗尽由 pipeline 在进 Decide 前就走 Finish）。用 focus 给用户一份局面速览（复用
	// formatFocus，决策行显示「状态不可达」），并整体标红以醒目；不写 log.Error——否则 zerolog 的 ERR
	// 会直接刷到用户界面；任务仍以错误中止。排查所需状态字段已在 recognition 的 "game state recognized" 日志里。
	if outcomes == nil || best == solver.ActionNone {
		red := "<span style=\"color:#ff4d4f;\">" + strings.ReplaceAll(formatFocus(gs, solver.ActionNone), "\n", "<br/>") + "</span>"
		maafocus.Print(ctx, red)
		return false
	}

	// 轮次边界状态转移先于路由落定（沿用原时序）：路由失败任务即中止，状态不再被读取。
	roundState.OnRoundEnd(best, gs.State)

	// 抽牌路径：按本次局面覆盖两个等待锚点（落位槽、战力点），
	// 避免后续抽牌沿用旧值（槽 1 早已在场、战力点已非 0）提前命中或失去重试保护。
	// 先覆盖再路由：两节点每次必经本动作，执行时必为最新定义。
	if best == solver.DrawCard {
		if err := overrideDrawCardNodes(ctx, gs.State.Hand); err != nil {
			log.Error().Err(err).Str("component", component).Msg("override draw card nodes failed")
			return false
		}
	}

	// 按决策路由到执行节点（节点自行点击 + 等动画），完成后回到 Decide 形成单步循环。
	if err := routeDecision(ctx, arg.CurrentTaskName, best); err != nil {
		log.Error().Err(err).Str("component", component).Str("action", best.String()).Msg("failed to route decision")
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
	maafocus.Print(ctx, formatFocus(gs, best))

	return true
}

// formatFocus 组装识别后唯一的 focus 文本：当前局面（手牌/牌库/演算次数/翻倍次数/放弃次数/翻倍态）+ 决策（下一步行为）。
// log 与 focus 分离——log 该写啥写啥，这里只给一份给人看的局面速览。
func formatFocus(gs GameState, best solver.Action) string {
	return i18n.T("trialofswordmancy.focus",
		handPointsDisplay(gs.HandRaw),
		deckDisplay(gs.Config.Deck),
		gs.State.RemainCalc,
		gs.State.RemainDouble,
		abandDisplay(gs.State.RemainAband),
		doubledText(gs.State.IsDoubled),
		actionFocusLabel(best),
	)
}

// handPointsDisplay 把各槽识别到的点数拼成逗号分隔串（跳过空槽 0）；全空返回「空」。
func handPointsDisplay(handRaw [5]int) string {
	var pts []string
	for _, p := range handRaw {
		if p != 0 {
			pts = append(pts, strconv.Itoa(p))
		}
	}
	if len(pts) == 0 {
		return i18n.T("trialofswordmancy.hand_empty")
	}
	return strings.Join(pts, ",")
}

// deckDisplay 把牌库构成拼成「点数:库存」串（点数 1-5 对应 Deck[0-4]）。
func deckDisplay(deck [5]int) string {
	parts := make([]string, 5)
	for i := 0; i < 5; i++ {
		parts[i] = fmt.Sprintf("%d:%d", i+1, deck[i])
	}
	return strings.Join(parts, " ")
}

// abandDisplay 返回剩余放弃次数文本；未知(-1)显示「?」。
func abandDisplay(remainAband int) string {
	if remainAband < 0 {
		return "?"
	}
	return strconv.Itoa(remainAband)
}

// doubledText 返回翻倍态中文标签。
func doubledText(isDoubled bool) string {
	if isDoubled {
		return i18n.T("trialofswordmancy.doubled")
	}
	return i18n.T("trialofswordmancy.undoubled")
}

// pickDecision 从各决策评估中选出要执行的动作，复刻 TS 计算器（trial-of-swordmancy-strategy.vue）
// 的推荐规则：取总价值最高者；但当最高者是「抽牌」且「放弃」的总价值与之相差 <1（并列）时改选「放弃」。
// 求解器自身 Policy 并列时按 [DrawCard,Abandon,Calculate] 取首位（=抽牌），与计算器展示的「并列优先放弃」不一致，
// 故在此覆盖。空 outcomes（不可达）返回 ActionNone。
func pickDecision(outcomes []solver.Outcome) solver.Action {
	if len(outcomes) == 0 {
		return solver.ActionNone
	}
	best := outcomes[0]
	for _, o := range outcomes {
		if o.Total > best.Total {
			best = o
		}
	}
	if best.Action == solver.DrawCard {
		for _, o := range outcomes {
			if o.Action == solver.Abandon && math.Abs(o.Total-best.Total) < 1 {
				return solver.Abandon
			}
		}
	}
	return best.Action
}

// routeDecision 把最优决策映射到执行节点，并用 OverrideNext 设置当前节点的 next。
// 实际点击/等待由各执行节点（DoDrawCard / DoDoubleReward / GiveUp / StartTrial）完成；
// Go 只负责决策与路由。仅处理 4 种真实决策；不可达（ActionNone）在调用前已 return false。
//
//   - DrawCard → DoDrawCard（点击抽牌按钮 + 第三抽弹窗 + 等动画）
//   - Double   → DoDoubleReward（点击翻倍按钮 + 等动画）
//   - Abandon  → GiveUp 链（放弃 → 确认 → 重置寻路 → 回主入口）
//   - Calculate→ StartTrial 战斗链
func routeDecision(ctx *maa.Context, currentNode string, action solver.Action) error {
	return ctx.OverrideNext(currentNode, []maa.NextItem{{Name: executeNode(action)}})
}

// overrideDrawCardNodes 按本次局面覆盖抽牌等待锚点——静态值在后续抽牌时失效：
// 落位槽递增（槽 1 早已在场），战力点变化（BattlePts0 只在战力点 0 时命中）。
//   - DoDrawCard.custom_action_param.wait_node → BattlePts<战力点 = 手牌点数总和 % 11>
//   - DoDrawCardSuccess.all_of → EnemyCard<落位槽 = 当前手牌数 + 1>（clamp [1,5] 纯属防御）
//
// 注：override 为字段级浅合并，custom_action_param 必须全量给出，与 Daily.json 的 DoDrawCard 同步维护。
func overrideDrawCardNodes(ctx *maa.Context, hand [5]int) error {
	slot := 1
	for _, c := range hand {
		slot += c
	}
	if slot > 5 {
		slot = 5
	}
	enemyCard := nodeEnemyCardPrefix + strconv.Itoa(slot)

	power := solver.PowerOf(hand)
	waitNode := nodeBattlePtsPrefix + strconv.Itoa(power)

	if err := ctx.OverridePipeline(map[string]any{
		nodeDoDrawCard: map[string]any{
			"custom_action_param": map[string]any{
				"action":       "Click",
				"interval_ms":  600,
				"repeat_count": 6,
				"wait_node":    waitNode,
			},
		},
		nodeDoDrawCardSuccess: map[string]any{
			"all_of": []string{enemyCard},
		},
	}); err != nil {
		return err
	}

	log.Debug().
		Str("component", component).
		Int("slot", slot).
		Str("allOf", enemyCard).
		Int("power", power).
		Str("waitNode", waitNode).
		Msg("draw card nodes overridden")
	return nil
}

// executeNode 把最优决策映射到执行节点名。
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
	}
	return "" // ActionNone 已在调用前 return false，此处不命中
}

// actionFocusLabel 返回决策的中文 UI 标签。
func actionFocusLabel(action solver.Action) string {
	switch action {
	case solver.DrawCard:
		return i18n.T("trialofswordmancy.action.drawcard")
	case solver.Abandon:
		return i18n.T("trialofswordmancy.action.abandon")
	case solver.Calculate:
		return i18n.T("trialofswordmancy.action.calculate")
	case solver.Double:
		return i18n.T("trialofswordmancy.action.double")
	default:
		return i18n.T("trialofswordmancy.action.unreachable")
	}
}

// decideOverflowMode 读取 Decide 节点 custom_action_param.overflowMode，是溢出模式的唯一数据源：
// RecognizeDeck 用它构造异步预求解的 cfg，Decide 用它覆盖 recognition 的默认值——同源同回退，
// 预求解配置与决策配置必然一致。参数是强制项：缺省/非法 = pipeline 配置错误，调用方硬中止，
// 不猜默认值（猜错会让预求解白算，且决策在无人察觉时换了模式）。
func decideOverflowMode(ctx *maa.Context) (solver.OverflowMode, bool) {
	raw, err := ctx.GetNodeJSON(nodeDecide)
	if err != nil {
		log.Error().Err(err).Str("component", component).Str("node", nodeDecide).Msg("GetNodeJSON failed")
		return solver.OverflowNone, false
	}
	return parseDecideOverflowMode(raw)
}

// parseDecideOverflowMode 从 GetNodeJSON 返回的节点 JSON 解析 custom_action_param.overflowMode。
// 注意 GetNodeJSON 返回的是 MaaFramework PipelineDumper 的规范化形态：action.param 是
// JCustomAction {target, target_offset, custom_action, custom_action_param}，玩家参数在
// custom_action_param 里，不是直接挂在 action.param 下（与 recognition.param 同级，见 nodeROI）。
func parseDecideOverflowMode(raw string) (solver.OverflowMode, bool) {
	var node struct {
		Action struct {
			Param struct {
				CustomActionParam struct {
					OverflowMode json.RawMessage `json:"overflowMode"`
				} `json:"custom_action_param"`
			} `json:"param"`
		} `json:"action"`
	}
	if err := json.Unmarshal([]byte(raw), &node); err != nil {
		log.Error().Err(err).Str("component", component).Str("node", nodeDecide).Msg("parse node JSON failed")
		return solver.OverflowNone, false
	}
	// RawMessage 判缺省：OverflowMode 的零值是合法模式（OverflowNone），按值无法区分「缺失」与「显式 OverflowNone」。
	if len(node.Action.Param.CustomActionParam.OverflowMode) == 0 {
		log.Error().Str("component", component).Str("node", nodeDecide).Msg("overflowMode param missing")
		return solver.OverflowNone, false
	}
	var mode solver.OverflowMode
	if err := json.Unmarshal(node.Action.Param.CustomActionParam.OverflowMode, &mode); err != nil {
		log.Error().Err(err).Str("component", component).Str("node", nodeDecide).Msg("invalid overflowMode value")
		return solver.OverflowNone, false
	}
	return mode, true
}

// —— 辅助：Custom 识别 detail 解包 ——

// unwrapCustomDetail 从 Custom 识别的 DetailJson 中取出我们写入的明文 JSON。
// 框架可能把它包成 {"best":{"detail": <raw>}}，两种形态都兼容。
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
