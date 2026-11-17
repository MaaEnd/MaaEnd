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

var _ maa.CustomActionRunner = &InitSolverAction{}

// InitSolverAction 按本轮牌库与溢出模式求解 MDP，结果存进 RoundState 供 DecideAction 使用。
// 由 TrialOfSwordmancyDailyRecognizeDeck 的动作调用，每轮演算一次。
type InitSolverAction struct{}

// Run 求解并缓存；配置未变时直接复用本轮已有的求解器。
func (a *InitSolverAction) Run(_ *maa.Context, arg *maa.CustomActionArg) bool {
	if arg == nil {
		log.Error().Str("component", component).Msg("custom action arg is nil")
		return false
	}

	mode, ok := parseOverflowMode(arg.CustomActionParam)
	if !ok {
		return false
	}

	deck, ok := roundState.Deck()
	if !ok {
		log.Error().Str("component", component).Msg("deck cache missing, cannot solve")
		return false
	}

	cfg := solver.DefaultConfig
	cfg.Deck = deck
	cfg.OverflowMode = mode

	if cur := roundState.Solver(); cur != nil && solver.ConfigKey(cur.Config()) == solver.ConfigKey(cfg) {
		return true
	}

	slv, err := solve(cfg)
	if err != nil {
		log.Error().Err(err).Str("component", component).Msg("solve failed")
		return false
	}
	roundState.setSolver(slv)

	log.Info().
		Str("component", component).
		Ints("deck", deck[:]).
		Str("overflowMode", mode.String()).
		Msg("solver ready")
	return true
}

// parseOverflowMode 读取本动作的 overflowMode 参数；缺失或非法即 pipeline 配置错误。
func parseOverflowMode(param string) (solver.OverflowMode, bool) {
	var p struct {
		OverflowMode *solver.OverflowMode `json:"overflowMode"`
	}
	if err := json.Unmarshal([]byte(param), &p); err != nil {
		log.Error().Err(err).Str("component", component).Msg("failed to parse solve param")
		return solver.OverflowNone, false
	}
	if p.OverflowMode == nil {
		log.Error().Str("component", component).Msg("overflowMode param missing")
		return solver.OverflowNone, false
	}
	return *p.OverflowMode, true
}

// solve 同步求解；panic 兜底为错误，回调里的 panic 会直接带走整个 go-service 进程。
func solve(cfg solver.Config) (slv *solver.Solver, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("solve panicked: %v", r)
		}
	}()
	slv = solver.NewSolver(cfg)
	slv.Solve()
	return slv, nil
}

var _ maa.CustomActionRunner = &DecideAction{}

// DecideAction 用本轮的求解器评估当前局面，取最优单步决策并路由到执行节点。
// 本身不持有状态：全部局面由识别器每步重新读出，唯一的副作用是轮次边界转移。
type DecideAction struct{}

// Run 决策并路由；局面不可达或本轮未求解即中止。
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

	slv := roundState.Solver()
	if slv == nil {
		log.Error().Str("component", component).Msg("solver missing, solve action not run this round")
		return false
	}

	best := pickDecision(slv.Decide(gs.State))
	deck, _ := roundState.Deck()

	// 不可达：识别产出了 MDP 状态空间外的局面（ROI/模板未校准、读错、手牌超牌库），是错误而非奖励耗尽。
	// 不打 log.Error：ERR 会被 zerolog 直接刷到用户界面。
	if best == solver.ActionNone {
		red := "<span style=\"color:#ff4d4f;\">" + strings.ReplaceAll(formatFocus(gs, deck, best), "\n", "<br/>") + "</span>"
		maafocus.Print(ctx, red)
		return false
	}

	roundState.OnRoundEnd(best, gs.State)

	if best == solver.DrawCard {
		if err := overrideDrawCardNodes(ctx, gs.State.Hand); err != nil {
			log.Error().Err(err).Str("component", component).Msg("override draw card nodes failed")
			return false
		}
	}

	// 溢出叹号受抽牌动画影响，改用识别阶段从牌库推导的手牌总分；inverse 每次开始演算都刷新。
	if best == solver.Calculate {
		if err := ctx.OverridePipeline(map[string]any{
			nodeFinishOnOverflow: map[string]any{
				"inverse": solver.TotalScore(gs.State.Hand) < 11,
			},
		}); err != nil {
			log.Error().Err(err).Str("component", component).Msg("override overflow state failed")
			return false
		}
	}

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
		Int("totalScore", solver.TotalScore(gs.State.Hand)).
		Msg("decision made")
	maafocus.Print(ctx, formatFocus(gs, deck, best))

	return true
}

// formatFocus 组装唯一的 focus 文本：当前局面 + 下一步决策。
func formatFocus(gs GameState, deck [5]int, best solver.Action) string {
	return i18n.T("trialofswordmancy.focus",
		handPointsDisplay(gs.HandRaw),
		deckDisplay(deck),
		gs.State.RemainCalc,
		gs.State.RemainDouble,
		abandDisplay(gs.State.RemainAband),
		doubledText(gs.State.IsDoubled),
		actionFocusLabel(best),
	)
}

// handPointsDisplay 把各槽点数拼成逗号分隔串（跳过空槽）；全空显示「空」。
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

func doubledText(isDoubled bool) string {
	if isDoubled {
		return i18n.T("trialofswordmancy.doubled")
	}
	return i18n.T("trialofswordmancy.undoubled")
}

// pickDecision 取总价值最高的决策；最高者是抽牌且放弃与之相差不到 1 时改选放弃，与前端的推荐规则一致。
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

// routeDecision 把决策映射到执行节点，用 OverrideNext 设置当前节点的 next；点击与等待由执行节点完成。
func routeDecision(ctx *maa.Context, currentNode string, action solver.Action) error {
	return ctx.OverrideNext(currentNode, []maa.NextItem{{Name: executeNode(action)}})
}

func executeNode(action solver.Action) string {
	switch action {
	case solver.DrawCard:
		return nodeDrawCard
	case solver.Double:
		return nodeDoDoubleReward
	case solver.Abandon:
		return nodeAbandon
	case solver.Calculate:
		return nodePreStartTrial
	}
	return ""
}

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

// unwrapCustomDetail 取出 Custom 识别 DetailJson 里我们写入的明文 JSON，兼容 {"best":{"detail":...}} 包裹。
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

const (
	// freeCalcMaxTotal 是 5 张铭牌点数之和的上限。
	freeCalcMaxTotal = 25
	// freeCalcMaxHand 是手牌落位槽数，即一轮最多能抽到的张数。
	freeCalcMaxHand = 5
)

// freeCalcParam 是 FreeCalcDecide 的目标点数区间，闭区间，按手牌点数之和计算。
type freeCalcParam struct {
	LowerBound int `json:"lower_bound"`
	UpperBound int `json:"upper_bound"`
}

var _ maa.CustomActionRunner = &FreeCalcDecideAction{}

// FreeCalcDecideAction 按目标点数区间做单步决策：已达标即开始演算，剩余牌库仍能凑进区间即抽牌，否则放弃。
// 手牌由识别器经 Detail 传来的总牌库与剩余牌库库存推导，本动作不重复识别。
type FreeCalcDecideAction struct{}

// Run 解析目标区间与牌库读数，判定单步决策并路由到执行节点。
func (a *FreeCalcDecideAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if ctx == nil || arg == nil {
		log.Error().Str("component", component).Msg("free calc decide context or arg is nil")
		return false
	}

	param, ok := parseFreeCalcParam(arg.CustomActionParam)
	if !ok {
		return false
	}

	reading, ok := parseDeckReading(arg.RecognitionDetail)
	if !ok {
		return false
	}

	// 整个总牌库都凑不出目标区间时，放弃也只会重开同一副牌，直接报错退出。
	if !canReachRange(reading.Deck, freeCalcMaxHand, param.LowerBound, param.UpperBound) {
		log.Error().
			Str("component", component).
			Int("lowerBound", param.LowerBound).
			Int("upperBound", param.UpperBound).
			Ints("deck", reading.Deck[:]).
			Msg("free calc target range unreachable with current deck")
		maafocus.Print(ctx, i18n.T("trialofswordmancy.free_calc_unreachable",
			param.LowerBound, param.UpperBound, deckDisplay(reading.Deck)))
		return false
	}

	hand, ok := deriveHand(reading.Deck, reading.RemainDeck)
	if !ok {
		log.Error().
			Str("component", component).
			Ints("deck", reading.Deck[:]).
			Ints("remainDeck", reading.RemainDeck[:]).
			Msg("derived hand invalid")
		return false
	}

	best := decideFreeCalc(hand, reading.RemainDeck, param)

	if best == solver.DrawCard {
		if err := overrideDrawCardNodes(ctx, hand); err != nil {
			log.Error().Err(err).Str("component", component).Msg("override draw card nodes failed")
			return false
		}
	}

	if err := routeDecision(ctx, arg.CurrentTaskName, best); err != nil {
		log.Error().Err(err).Str("component", component).Str("action", best.String()).Msg("failed to route free calc decision")
		return false
	}

	log.Info().
		Str("component", component).
		Str("action", best.String()).
		Int("lowerBound", param.LowerBound).
		Int("upperBound", param.UpperBound).
		Int("totalScore", solver.TotalScore(hand)).
		Ints("hand", hand[:]).
		Ints("remainDeck", reading.RemainDeck[:]).
		Msg("free calc decision made")

	return true
}

// parseFreeCalcParam 读取目标点数区间；越界或倒置即 pipeline 配置错误。
func parseFreeCalcParam(param string) (freeCalcParam, bool) {
	var p freeCalcParam
	if err := json.Unmarshal([]byte(param), &p); err != nil {
		log.Error().Err(err).Str("component", component).Msg("failed to parse free calc param")
		return freeCalcParam{}, false
	}
	if p.LowerBound < 1 || p.UpperBound > freeCalcMaxTotal || p.LowerBound > p.UpperBound {
		log.Error().
			Str("component", component).
			Int("lowerBound", p.LowerBound).
			Int("upperBound", p.UpperBound).
			Msg("free calc target range is invalid")
		return freeCalcParam{}, false
	}
	return p, true
}

// parseDeckReading 取出 RecognizeDeck 写进 Detail 的牌库读数。
func parseDeckReading(detail *maa.RecognitionDetail) (deckReading, bool) {
	detailJSON := unwrapCustomDetail(detail)
	if detailJSON == "" {
		log.Error().Str("component", component).Msg("recognition detail json is empty")
		return deckReading{}, false
	}
	var reading deckReading
	if err := json.Unmarshal([]byte(detailJSON), &reading); err != nil {
		log.Error().Err(err).Str("component", component).Msg("failed to parse deck reading")
		return deckReading{}, false
	}
	return reading, true
}

// decideFreeCalc 按闭区间目标判定单步决策：达标即演算；低于下界且仍能凑进区间就抽牌；其余（超出上界或已凑不出）放弃。
func decideFreeCalc(hand, remainDeck [5]int, param freeCalcParam) solver.Action {
	total := solver.TotalScore(hand)
	if total >= param.LowerBound && total <= param.UpperBound {
		return solver.Calculate
	}
	if total < param.LowerBound &&
		canReachRange(remainDeck, freeCalcMaxHand-handCount(hand), param.LowerBound-total, param.UpperBound-total) {
		return solver.DrawCard
	}
	return solver.Abandon
}

// canReachRange 判断剩余牌库里存在张数不超过 maxDraws、点数和落在 [low, high] 的抽取组合。
func canReachRange(remainDeck [5]int, maxDraws, low, high int) bool {
	if maxDraws <= 0 {
		return false
	}

	// reach[draws][sum]：抽 draws 张凑出 sum 是否可行（每张牌按库存逐张做 0/1 背包）。
	var reach [freeCalcMaxHand + 1][freeCalcMaxTotal + 1]bool
	reach[0][0] = true
	for point := 1; point <= 5; point++ {
		for count := 0; count < remainDeck[point-1] && count < maxDraws; count++ {
			for draws := maxDraws - 1; draws >= 0; draws-- {
				for sum := freeCalcMaxTotal - point; sum >= 0; sum-- {
					if reach[draws][sum] {
						reach[draws+1][sum+point] = true
					}
				}
			}
		}
	}

	for draws := 1; draws <= maxDraws; draws++ {
		for sum := 0; sum <= freeCalcMaxTotal; sum++ {
			if reach[draws][sum] && sum >= low && sum <= high {
				return true
			}
		}
	}
	return false
}

func handCount(hand [5]int) (n int) {
	for _, c := range hand {
		n += c
	}
	return n
}
