package trialofswordmancy

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

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

	// 配置全部来自 recognition 识别（牌库/溢出模式/手牌/剩余次数/翻倍态皆从截图读出），
	// 本节点不带 custom_action_param，无需再从节点加载。
	cfg := gs.Config

	slv := solverFor(cfg)
	outcomes := slv.Decide(gs.State)

	var best solver.Action
	for _, o := range outcomes {
		if o.IsBest {
			best = o.Action
			break
		}
	}

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

// formatFocus 组装识别后唯一的 focus 文本：当前局面（手牌/牌库/演算次数/翻倍次数/翻倍态）+ 决策（下一步行为）+ 路由方向。
// log 与 focus 分离——log 该写啥写啥，这里只给一份给人看的局面速览。
func formatFocus(gs GameState, best solver.Action) string {
	return fmt.Sprintf(
		"选剑演武 | 手牌 %s | 牌库 %s | 演算%d 翻倍%d %s | → %s（%s）",
		handPointsDisplay(gs.HandRaw),
		deckDisplay(gs.Config.Deck),
		gs.State.RemainCalc,
		gs.State.RemainDouble,
		doubledText(gs.State.IsDoubled),
		actionFocusLabel(best),
		executeNode(best),
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

// doubledText 返回翻倍态中文标签。
func doubledText(isDoubled bool) string {
	if isDoubled {
		return "已翻倍"
	}
	return "未翻倍"
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
