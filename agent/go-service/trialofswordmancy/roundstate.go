package trialofswordmancy

import (
	"github.com/MaaXYZ/MaaEnd/agent/go-service/trialofswordmancy/solver"
	"github.com/rs/zerolog/log"
)

// —— 轮次状态：本轮已知 + 本步推导 + 轮次边界 ——

// RoundState 是选剑演武的「轮次状态」：一轮内会变的运行知识、单步局面推导与轮次转移。
//
// 轮次是生命周期单位：一轮内牌库恒定，轮次结束（放弃/演算）即失效——所以这里的缓存随轮次重置，
// 与 solver 备忘（presolve.go，进程级、永不重置）相反。
//
// deck 只在轮次开始时完整识别一次：Hand 模板匹配不稳定，不能每步跑，之后每步只读
// remainDeck OCR 反推手牌（deriveGameState）。aband 界面不显示，只有放弃弹窗里有，
// 故由 RecognizeAband 探测写入、真实放弃时递减。
//
// 放弃消耗规则在 solver/state.go transitions()（MDP 建模侧）另有一份镜像拷贝；
// 刻意不派生——MDP 建模与运行簿记是两种语境，改动需两侧同步（注释互指）。
//
// 无需加锁：MaaFramework 保证任务回调单线程；预求解 goroutine 不碰本结构（presolve.go）。
type RoundState struct {
	deck    [5]int // 牌库总牌量；deckSet=false = 未设置
	deckSet bool   // 显式标志而非零值哨兵：setDeck([5]int{}) 不应静默失效
	aband   int    // -1 = 未知，需放弃弹窗探测
}

// roundState 是生产路径的单例：MaaFramework 回调模型下 Custom 组件无状态，
// 跨识别器共享状态只能靠包级实例。
var roundState = &RoundState{aband: -1}

// crossDayRemainCalc 标识「跨日残局」：系统白送的一局，放弃不消耗放弃次数。
// 刻意不引用 solver.MaxRemainCalc——那边是状态空间界限，这边是轮次身份，只是碰巧同值。
const crossDayRemainCalc = 4

// Deck 返回牌库；ok=false = 未设置（漏跑 RecognizeDeck，或已随轮次结束重置）。
func (s *RoundState) Deck() (deck [5]int, ok bool) {
	if !s.deckSet {
		return s.deck, false
	}
	return s.deck, true
}

func (s *RoundState) setDeck(deck [5]int) {
	s.deck = deck
	s.deckSet = true
}

// resetDeck 使牌库失效：下一轮是新洗的牌库，旧值必须作废——总成识别依赖
// 缓存缺失时的严格中止兜底（Recognition.Run），正确性不靠「推导越界」拦截。
func (s *RoundState) resetDeck() {
	s.deck = [5]int{}
	s.deckSet = false
	log.Debug().Str("component", component).Msg("deck cache reset")
}

// Aband 返回剩余放弃次数；-1 = 未知。
func (s *RoundState) Aband() int {
	return s.aband
}

func (s *RoundState) setAband(n int) {
	s.aband = n
}

// OnRoundEnd 是轮次边界规则的唯一 owner。
//
// 放弃：跨日残局（crossDayRemainCalc）白送不扣；已用完（0）也不减——0 减成 -1 会把
// 「未知」哨兵写回来，而 AbandProbe 每轮只在开始时探测一次。放弃/演算都会结束本轮，
// 牌库必须失效。规则本体在 roundEndTransition 纯函数里。
func (s *RoundState) OnRoundEnd(action solver.Action, st solver.State) {
	newAband, reset := roundEndTransition(s.aband, action, st)
	s.aband = newAband
	if reset {
		s.resetDeck()
	}
}

// roundEndTransition 是轮次结束转移的纯函数（规则见 OnRoundEnd），
// 规则本体与 roundState 落定分离，便于独立审查。
func roundEndTransition(aband int, action solver.Action, st solver.State) (newAband int, resetDeck bool) {
	if action == solver.Abandon && st.RemainCalc != crossDayRemainCalc && aband > 0 {
		aband--
	}
	return aband, action == solver.Abandon || action == solver.Calculate
}

// —— 本步局面推导（纯函数） ——

// stepReadings 是单步读数的原始汇总，喂给 deriveGameState 做纯推导。
// 只含「读到了什么」，不含推导规则——读数来源分散在 Recognition.Run 的缓存与 OCR/模板
// 调用里，这个结构就是适配与纯推导之间的契约。
type stepReadings struct {
	deck         [5]int // 轮次内恒定的缓存总牌量
	remainDeck   [5]int
	remainCalc   int
	remainAband  int // -1 = 未知（透传给求解器判不可达）
	remainDouble int
	isDoubled    bool
}

// deriveGameState 把读数推导为 GameState（纯函数，无 ctx / 无缓存依赖）。
//
// 唯一失败面：deck − remainDeck 矛盾（负值或总张数 > 5）——两个读数必有一个是错的，
// 不在矛盾信息上做决策，ok=false 由调用方中止。模型外的自洽读数（如演算次数 4）不算失败：
// 状态合法性由 solver.stateFilter 声明，越界值原样送入、由求解器判不可达（ADR-0001）。
//
// 演算次数 +1 偏移：屏幕显示「进行中这局之外」的剩余，进抽牌界面即扣 1，而求解器把
// 进行中这局也算可用。仅演算次数有此偏移（放弃/翻倍界面就是真实值）。
// 跨天残局白送：OCR 读到 3 → RemainCalc=4，由求解器按特殊规则处理。
func deriveGameState(r stepReadings) (GameState, bool) {
	// 推导手牌并校验：违例 = remainDeck OCR 抖动或缓存过期，显式中止；
	// 日志带缓存 Deck 与刚读的 remainDeck 供对账。
	var handCounts [5]int
	for i := 0; i < 5; i++ {
		handCounts[i] = r.deck[i] - r.remainDeck[i]
	}
	if !validHand(handCounts) {
		return GameState{}, false
	}

	// 推导路径没有槽位级识别结果：按点数升序合成 HandRaw 供 focus 展示（不影响求解）。
	handRaw := synthesizeHandRaw(handCounts)

	cfg := solver.DefaultConfig
	cfg.Deck = r.deck

	state := solver.State{
		RemainCalc:   r.remainCalc + 1,
		RemainAband:  r.remainAband,
		RemainDouble: r.remainDouble,
		IsDoubled:    r.isDoubled,
		Hand:         handCounts,
	}
	return GameState{
		State:   state,
		Config:  cfg,
		HandRaw: handRaw,
	}, true
}

// validHand 校验手牌计数合法性：各点数非负且总张数 ≤ 5。
func validHand(hand [5]int) bool {
	total := 0
	for _, c := range hand {
		if c < 0 {
			return false
		}
		total += c
	}
	return total <= 5
}

// synthesizeHandRaw 从手牌计数合成槽位展示数组（点数升序填槽，0=空槽）。
// 推导路径没有槽位级识别结果；HandRaw 仅用于 focus 展示，槽位顺序不影响求解。
func synthesizeHandRaw(hand [5]int) (handRaw [5]int) {
	slot := 0
	for point := 1; point <= 5 && slot < 5; point++ {
		for c := 0; c < hand[point-1] && slot < 5; c++ {
			handRaw[slot] = point
			slot++
		}
	}
	return handRaw
}
