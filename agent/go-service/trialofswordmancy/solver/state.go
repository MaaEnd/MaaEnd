package solver

import "fmt"

// mdpState 是 solver 内部的 MDP 状态：在对外 State 之外多一个吸收态标志。
// 整个状态空间里只有 1 个吸收态节点（恒为 States[0]），判定条件 = RemainCalc==0。
//
// 不对外导出，避免与对外的 State 同名冲突（迁移文档 §4.4）。
type mdpState struct {
	isEnd bool // 是否为吸收态（演算耗尽）
	State      // isEnd 为 true 时为零值，无意义
}

// endState 是唯一的吸收态实例。
func endState() mdpState { return mdpState{isEnd: true} }

// mdpStateKey 把状态序列化为唯一字符串，用作 map key。
//
// 吸收态恒为 "END"。过渡态格式与 TypeScript 源码 状态键 完全一致，便于交叉验证：
//
//	"{RemainCalc},{RemainAband},{RemainDouble},{IsDoubled},{h0},{h1},{h2},{h3},{h4}"
//
// 其中 IsDoubled 用 "true"/"false"（Go %t 与 JS 模板字符串一致）。
func mdpStateKey(s mdpState) string {
	if s.isEnd {
		return "END"
	}
	h := s.Hand
	return fmt.Sprintf("%d,%d,%d,%t,%d,%d,%d,%d,%d",
		s.RemainCalc, s.RemainAband, s.RemainDouble, s.IsDoubled,
		h[0], h[1], h[2], h[3], h[4])
}

// TotalScore 计算总分：Σ (i+1) * hand[i]，i 从 0..4（点数 = i+1）。
func TotalScore(hand [5]int) int {
	sum := 0
	for i, count := range hand {
		sum += (i + 1) * count
	}
	return sum
}

// PowerOf 计算战力点 = 总分 % 11，范围恒为 0..10。
func PowerOf(hand [5]int) int {
	return TotalScore(hand) % 11
}

// handTotal 返回手牌总张数 Σ hand[i]。
func handTotal(hand [5]int) int {
	total := 0
	for _, c := range hand {
		total += c
	}
	return total
}

// pure 工具函数（不依赖 Solver 配置）在上面。以下为依赖 Solver 配置的方法。

// handCombinations 枚举所有「各点数张数」组合：约束总张数 ≤ 5，
// 且每点数张数不超过 Deck[i]（§6.2）。
func (s *Solver) handCombinations() [][5]int {
	var results [][5]int
	var rec func(idx int, current [5]int, total int)
	rec = func(idx int, current [5]int, total int) {
		if idx == len(s.cfg.Deck) {
			if total <= 5 {
				results = append(results, current)
			}
			return
		}
		for i := 0; i <= s.cfg.Deck[idx] && total+i <= 5; i++ {
			current[idx] = i
			rec(idx+1, current, total+i)
		}
	}
	rec(0, [5]int{}, 0)
	return results
}

// calcSettleReward 计算演算奖励（§6.3）：先按溢出模式判定是否归零，
// 再按战力点查表，若已翻倍则 ×2。
func (s *Solver) calcSettleReward(hand [5]int, isDoubled bool) int {
	sum := TotalScore(hand)
	switch s.cfg.OverflowMode {
	case OverflowNone:
		if sum >= 11 {
			return 0
		}
	case OverflowOnce:
		if sum >= 22 {
			return 0
		}
	case OverflowTwice:
		// 无上限
	}
	power := PowerOf(hand) // 恒为 0..10
	r := 0
	if power >= 0 && power < len(s.cfg.Reward) {
		r = s.cfg.Reward[power]
	}
	if isDoubled {
		r *= 2
	}
	return r
}

// getState 构造状态：RemainCalc==0 时坍缩为唯一吸收态（§6.4）。
func (s *Solver) getState(remainCalc, remainAband, remainDouble int, isDoubled bool, hand [5]int) mdpState {
	if remainCalc == 0 {
		return endState()
	}
	return mdpState{State: State{
		RemainCalc:   remainCalc,
		RemainAband:  remainAband,
		RemainDouble: remainDouble,
		IsDoubled:    isDoubled,
		Hand:         hand,
	}}
}

// transition 是一条状态转移：目标状态 + 概率。
type transition struct {
	state mdpState
	prob  float64
}

// transitions 返回 from 在 action 下的 [(目标状态, 概率)]，并过滤概率为 0 的项（§6.5）。
//
// 吸收态自循环：返回 [(吸收态, 1.0)]。
func (s *Solver) transitions(from mdpState, action Action) []transition {
	if from.isEnd {
		return []transition{{state: from, prob: 1.0}}
	}

	st := from.State
	var raw []transition

	switch action {
	case DrawCard:
		// 按剩余牌库概率抽一张。
		// 注：totalRemain 不会为 0——空间内状态恒有 Hand[i] <= Deck[i]（handCombinations 构造时卡死），
		// 故 remain[i] >= 0；而下面的除法被 `remain[i] > 0` 守卫，能进入除法说明至少一个 remain[i] > 0，
		// 即 totalRemain > 0，不会除零产生 +Inf。
		var remain [5]int
		totalRemain := 0
		for i := 0; i < len(remain); i++ {
			remain[i] = s.cfg.Deck[i] - st.Hand[i]
			totalRemain += remain[i]
		}
		for i := 0; i < len(remain); i++ {
			if remain[i] > 0 {
				p := float64(remain[i]) / float64(totalRemain)
				targetHand := st.Hand
				targetHand[i]++
				target := s.getState(st.RemainCalc, st.RemainAband, st.RemainDouble, st.IsDoubled, targetHand)
				raw = append(raw, transition{state: target, prob: p})
			}
		}

	case Abandon:
		// 清空手牌；翻倍状态清除但翻倍次数不消耗。
		// 剩余放弃次数 > 0 时扣放弃次数，否则扣演算次数。
		var target mdpState
		if st.RemainAband > 0 {
			target = s.getState(st.RemainCalc, st.RemainAband-1, st.RemainDouble, false, [5]int{})
		} else {
			target = s.getState(st.RemainCalc-1, st.RemainAband, st.RemainDouble, false, [5]int{})
		}
		raw = append(raw, transition{state: target, prob: 1.0})

	case Calculate:
		// 结算：消耗演算次数；若已翻倍则消耗一次翻倍次数。
		nextDouble := st.RemainDouble
		if st.IsDoubled {
			nextDouble--
		}
		target := s.getState(st.RemainCalc-1, st.RemainAband, nextDouble, false, [5]int{})
		raw = append(raw, transition{state: target, prob: 1.0})

	case Double:
		// 仅置位翻倍标志，不消耗任何次数（消耗发生在演算时）
		target := s.getState(st.RemainCalc, st.RemainAband, st.RemainDouble, true, st.Hand)
		raw = append(raw, transition{state: target, prob: 1.0})
	}

	out := make([]transition, 0, len(raw))
	for _, t := range raw {
		if t.prob > 0 {
			out = append(out, t)
		}
	}
	return out
}

// allowedActions 返回状态下的合法决策集合（§6.6）。
//
// 顺序与 TypeScript 源码 状态容许决策 一致，保证并列最优（受 1e-10 阈值过滤后）
// 的取舍结果与 TS 版逐位相同。
func (s *Solver) allowedActions(st mdpState) []Action {
	if st.isEnd {
		return nil
	}
	total := handTotal(st.Hand)
	if total == 5 {
		// 满手牌只能演算或放弃
		return []Action{Calculate, Abandon}
	}
	base := []Action{DrawCard, Abandon, Calculate}
	if total == 2 && !st.IsDoubled && st.RemainDouble > 0 {
		base = append(base, Double)
	}
	return base
}

// immediateReward 返回 (state, action) 的即时奖励（§6.7）。
// 仅「开始演算」产生即时奖励（结算奖励）；其余决策即时奖励为 0。
func (s *Solver) immediateReward(st mdpState, action Action) int {
	if st.isEnd {
		return 0
	}
	if action == Calculate {
		return s.calcSettleReward(st.Hand, st.IsDoubled)
	}
	return 0
}

// stateFilter 判定过渡态是否需要纳入状态空间（§6.8，最易抄错，逐条对齐）。
func (s *Solver) stateFilter(st State) bool {
	if !(st.RemainCalc >= 1 && st.RemainCalc <= 3) {
		return false
	}
	if !(st.RemainAband >= 0 && st.RemainAband <= 3) {
		return false
	}
	// 第 3 条：剩余翻倍次数 - 3 + maxDouble <= 剩余翻倍次数 <= maxDouble
	// 语义等价于「已消耗翻倍次数 ≤ 已消耗演算次数」，排除翻倍用得比演算还多的非法态。
	if !(st.RemainCalc-3+s.cfg.MaxDouble <= st.RemainDouble && st.RemainDouble <= s.cfg.MaxDouble) {
		return false
	}
	total := handTotal(st.Hand)
	if total <= 1 && st.IsDoubled {
		return false
	}
	if st.IsDoubled && st.RemainDouble == 0 {
		return false
	}
	return true
}

// buildStateList 枚举状态空间（§6.9）。吸收态恒为 States[0]（Index["END"]==0）。
func (s *Solver) buildStateList() {
	s.states = []mdpState{endState()}
	s.index = map[string]int{mdpStateKey(endState()): 0}

	combos := s.handCombinations()
	for remainCalc := 1; remainCalc <= 3; remainCalc++ {
		for remainAband := 0; remainAband <= 3; remainAband++ {
			for remainDouble := 0; remainDouble <= s.cfg.MaxDouble; remainDouble++ {
				for _, isDoubled := range []bool{false, true} {
					for _, hand := range combos {
						st := State{
							RemainCalc:   remainCalc,
							RemainAband:  remainAband,
							RemainDouble: remainDouble,
							IsDoubled:    isDoubled,
							Hand:         hand,
						}
						if !s.stateFilter(st) {
							continue
						}
						ms := mdpState{State: st}
						key := mdpStateKey(ms)
						if _, ok := s.index[key]; !ok {
							s.index[key] = len(s.states)
							s.states = append(s.states, ms)
						}
					}
				}
			}
		}
	}
}
