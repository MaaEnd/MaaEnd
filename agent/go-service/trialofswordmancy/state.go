package trialofswordmancy

import (
	"github.com/MaaXYZ/MaaEnd/agent/go-service/trialofswordmancy/solver"
	"github.com/rs/zerolog/log"
)

// RoundState 保存一轮演算内的状态：牌库、剩余放弃次数与本轮求解器。
// 牌库与求解器都是轮次级数据，轮次结束（放弃/演算）即作废；无需加锁，任务回调单线程。
type RoundState struct {
	deck    [5]int
	deckSet bool
	aband   int
	solver  *solver.Solver
}

var roundState = &RoundState{aband: -1}

// crossDayRemainCalc 是跨日残局的剩余演算次数：系统白送的一局，放弃不消耗放弃次数。
const crossDayRemainCalc = 4

// Deck 返回本轮牌库；ok=false 表示未识别或已随轮次结束作废。
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

// Aband 返回剩余放弃次数；-1 表示尚未从放弃弹窗探测到。
func (s *RoundState) Aband() int {
	return s.aband
}

func (s *RoundState) setAband(n int) {
	s.aband = n
}

// Solver 返回本轮求解器；nil 表示本轮尚未求解。
func (s *RoundState) Solver() *solver.Solver {
	return s.solver
}

func (s *RoundState) setSolver(slv *solver.Solver) {
	s.solver = slv
}

// OnRoundEnd 落定轮次边界：放弃扣一次放弃次数（跨日残局与已用完 0 次不扣），牌库与求解器作废。
func (s *RoundState) OnRoundEnd(action solver.Action, st solver.State) {
	newAband, reset := roundEndTransition(s.aband, action, st)
	s.aband = newAband
	if reset {
		s.resetRound()
	}
}

func roundEndTransition(aband int, action solver.Action, st solver.State) (newAband int, reset bool) {
	if action == solver.Abandon && st.RemainCalc != crossDayRemainCalc && aband > 0 {
		aband--
	}
	return aband, action == solver.Abandon || action == solver.Calculate
}

func (s *RoundState) resetRound() {
	s.deck = [5]int{}
	s.deckSet = false
	s.solver = nil
	log.Debug().Str("component", component).Msg("round state reset")
}
