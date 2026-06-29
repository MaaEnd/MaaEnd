package trialofswordmancy

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/maafocus"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/trialofswordmancy/solver"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

// —— Decide 动作 ——

var _ maa.CustomActionRunner = &DecideAction{}

// remainCalcEndgame 标志跨天残局：求解器态空间 RemainCalc 上界 3（每日演算上限），
// recognition 用 OCR+1 还原后，残局那局 OCR 读到 3 → RemainCalc=4，超出态空间。残局不求解，
// Decide 直接放弃这局（见 DecideAction.Run）。
const remainCalcEndgame = 4

// DecideAction 反序列化 recognition 产出的 GameState，调 solver.Decide 取最优单步决策，
// 按决策用 OverrideNext 路由到执行节点。
//
// 几乎无状态：每步的完整 State 都由 recognition 读出后传入；本动作只做「求解 → 路由」。
// 唯一副作用：路由到 放弃/开始演算（回合结束）时 resetAband()（放弃会扣1次致缓存失效）。
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

	// 跨天残局：recognition 读出 RemainCalc=4（OCR=3，超出求解器态空间上界 3=每日演算上限）。
	// 残局那局白送、且放弃只扣放弃次数不扣演算次数——跳过求解，直接放弃这局，回到主入口开正常的 3 局。
	// （求解器态空间不支持 4 层；故残局直接放弃，不在 recognition 钳制近似。）
	if gs.State.RemainCalc == remainCalcEndgame {
		resetAband() // 放弃扣 1 次放弃次数，缓存失效，下回合首步重新探测
		if err := routeDecision(ctx, arg.CurrentTaskName, solver.Abandon); err != nil {
			log.Error().Err(err).Str("component", component).Msg("failed to route endgame give-up")
			return false
		}
		log.Info().
			Str("component", component).
			Int("remainCalc", gs.State.RemainCalc).
			Msg("cross-day endgame (RemainCalc=4): skip solver, give up the free run")
		maafocus.Print(ctx, "选剑演武\n跨天残局\n→ 放弃本局")
		return true
	}

	// 配置：牌库/手牌/剩余次数/翻倍态来自 recognition 截图识别；溢出模式是玩家策略选项，
	// 由本节点 custom_action_param.overflowMode 提供（任务 select 决定），覆盖 recognition 的默认值。
	cfg := gs.Config
	cfg.OverflowMode = loadOverflowMode(arg.CustomActionParam)

	slv := solverFor(cfg)
	outcomes := slv.Decide(gs.State)

	// 不直接用求解器 Policy（其并列时按 [DrawCard,Abandon,Calculate] 取首位=抽牌）；
	// 复刻 TS 计算器（trial-of-swordmancy-strategy.vue）的推荐规则：抽牌与放弃总价值差 <1 时优先放弃。
	best := pickDecision(outcomes)

	// 不可达：识别产出了不在 MDP 状态空间的局面（识别 ROI/模板未校准、读错、或手牌超牌库等）。
	// 这是错误，不是「奖励耗尽」—— 奖励耗尽由 pipeline 在进 Decide 前就识别并走 Finish。
	// 这里直接让动作失败（return false），任务以错误中止，不冒充正常结束。
	if outcomes == nil || best == solver.ActionNone {
		log.Error().
			Str("component", component).
			Ints("hand", gs.State.Hand[:]).
			Int("remainCalc", gs.State.RemainCalc).
			Int("remainAband", gs.State.RemainAband).
			Int("remainDouble", gs.State.RemainDouble).
			Bool("isDoubled", gs.State.IsDoubled).
			Ints("deck", gs.Config.Deck[:]).
			Msg("unreachable state: recognition produced a state outside the MDP space; aborting")
		maafocus.Print(ctx, "选剑演武：识别失败")
		return false
	}

	// 放弃/开始演算会结束当前回合：放弃还会扣 1 次放弃次数（缓存失效）。重置为 -1，下回合首步重新探测。
	if best == solver.Abandon || best == solver.Calculate {
		resetAband()
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
	return fmt.Sprintf(
		"选剑演武\n手牌 %s\n牌库 %s\n演算%d 翻倍%d 放弃%s %s\n→ %s",
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
		return "空"
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
		return "已翻倍"
	}
	return "未翻倍"
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

// loadOverflowMode 从 Decide 节点的 custom_action_param 解析溢出模式；
// 缺省或解析失败 → OverflowNone（不接受溢出，默认）。这是玩家策略选项（任务 select 决定），
// 不属于截图识别范畴，故由 action 提供、覆盖 recognition 的默认。
func loadOverflowMode(customActionParam string) solver.OverflowMode {
	if customActionParam == "" {
		return solver.OverflowNone
	}
	var p struct {
		OverflowMode solver.OverflowMode `json:"overflowMode"`
	}
	if err := json.Unmarshal([]byte(customActionParam), &p); err != nil {
		log.Warn().Err(err).Str("component", component).Msg("parse overflowMode custom_action_param 失败，回退 OverflowNone")
		return solver.OverflowNone
	}
	return p.OverflowMode
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
