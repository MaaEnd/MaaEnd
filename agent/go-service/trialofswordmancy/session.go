package trialofswordmancy

import (
	"github.com/MaaXYZ/MaaEnd/agent/go-service/trialofswordmancy/solver"
	"github.com/rs/zerolog/log"
)

// Session 是一次任务运行内会变的状态（run-scoped）。
//
// 与 solver 备忘（presolve.go，进程级、按 Config 键控、永不重置）不同，这里三项都以轮次为生命周期：
//   - deck：一轮演算内牌库恒定，而完整识别要跑不稳定的 Hand 模板匹配，所以只在轮次开始时
//     由 RecognizeDeck 完整识别一次写入，轮次结束（放弃/演算）时重置。
//   - aband：界面上不直接显示剩余放弃次数，只有放弃确认弹窗里才写，故由 RecognizeAband
//     探测写入、真实放弃时递减，直到下次任务运行重新探测。
//   - future：本轮异步预求解的结果载体（presolve.go），RecognizeDeck 写入、首个 Decide
//     取用后即清除；nil = 预热时缓存已热或本轮已消费。一轮里有多个 Decide 步，只有第一个
//     需要等 goroutine——消费完成即缓存已热，后续 Decide 走 solverFor 直接命中（awaitPreSolve）。
//
// 放弃消耗规则在 solver/state.go 的 transitions()（MDP 建模侧）里还有一份镜像拷贝；
// 刻意保留两份而非从 solver 派生——MDP 建模与运行簿记是两种语境，改动需两侧同步，注释互指。
//
// MaaFramework 保证任务回调单线程同步执行，无需加锁；后台预求解 goroutine 不碰本结构
// （见 presolve.go spawnPreSolve 的线程亲和说明）。
type Session struct {
	deck   [5]int // 零值 = 未设置：游戏内牌库恒非空（默认 [4,5,6,6,7]），全零不可能是合法识别结果
	aband  int    // -1 = 未知，需放弃弹窗探测
	future *solveFuture
}

// sess 是生产路径的单例：各 Custom 组件是 MaaFramework 回调模型下的无状态 struct，
// 跨识别器共享状态只能靠包级实例。
var sess = &Session{aband: -1}

// crossDayRemainCalc 标识「跨日残局」那局：系统白送的一局，放弃不消耗放弃次数。
// 刻意不引用 solver.MaxRemainCalc——那边是状态空间界限，这边是轮次身份，只是碰巧同值。
const crossDayRemainCalc = 4

// Deck 返回牌库；ok=false 表示未设置（轮次开始前漏跑 RecognizeDeck，或已随轮次结束重置）。
func (s *Session) Deck() (deck [5]int, ok bool) {
	if s.deck == [5]int{} {
		return s.deck, false
	}
	return s.deck, true
}

// SetDeck 写入完整识别出的牌库。
func (s *Session) SetDeck(deck [5]int) {
	s.deck = deck
}

// ResetDeck 使牌库回到未设置态：下一轮是新洗的牌库，旧值必须失效——总成识别依赖
// 缓存缺失时的严格中止兜底（见 Recognition.Run），正确性不应依赖「推导越界」那层拦截。
func (s *Session) ResetDeck() {
	s.deck = [5]int{}
	log.Debug().Str("component", component).Msg("deck cache reset")
}

// Aband 返回剩余放弃次数；-1 = 未知。
func (s *Session) Aband() int {
	return s.aband
}

// SetAband 写入从放弃弹窗识别出的剩余次数。
func (s *Session) SetAband(n int) {
	s.aband = n
}

// SetPendingSolve 写入本轮异步预求解的结果载体；nil = 预热时缓存已热，Decide 无需等待。
func (s *Session) SetPendingSolve(f *solveFuture) {
	s.future = f
}

// PendingSolve 返回本轮异步预求解的结果载体（presolve.go awaitPreSolve 取用）；nil = 预热时缓存已热或已消费。
func (s *Session) PendingSolve() *solveFuture {
	return s.future
}

// OnRoundEnd 落定轮次结束时的状态转移，是轮次边界规则的唯一 owner。
//
// 放弃：消耗一次放弃次数；跨日残局那局（crossDayRemainCalc）白送不扣；已用完（0）也不减——
// 0 减成 -1 会把「未知」哨兵写回来，而 AbandProbe 每轮只在开始时探测一次。
// 放弃/演算都会结束本轮：牌库必须失效（见 ResetDeck）。
func (s *Session) OnRoundEnd(action solver.Action, st solver.State) {
	if action == solver.Abandon && st.RemainCalc != crossDayRemainCalc && s.aband > 0 {
		s.aband--
	}
	if action == solver.Abandon || action == solver.Calculate {
		s.ResetDeck()
	}
}
