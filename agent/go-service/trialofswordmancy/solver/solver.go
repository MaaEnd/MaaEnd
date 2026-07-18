package solver

import (
	"errors"
	"fmt"
	"math"
	"sort"
)

// Solver 是选剑演武 MDP 求解器，持有基础设定 Config。
//
// 两段式用法（与 TypeScript 源码 .vue 调用范式一致）：
//   - 基础设定变 → NewSolver(cfg).Solve()，缓存 *Solver（较重，状态空间数百~上千）。
//   - 每步查询状态变 → 复用缓存的 *Solver，调 Decide(State) / Best(State)（极快，纯查表）。
type Solver struct {
	cfg      Config
	states   []mdpState
	index    map[string]int
	solved   bool
	solution *Solution
}

// NewSolver 用给定基础设定构造求解器（尚未求解）。
func NewSolver(cfg Config) *Solver {
	return &Solver{cfg: cfg}
}

// Config 返回求解器的基础设定。
func (s *Solver) Config() Config { return s.cfg }

// Solve 全量求解 MDP（§6.10）：构建反向邻接表 → 反向 BFS 求 DAG 最长路得到拓扑序
// → 按 dist 升序做 Bellman 最优 DP（1e-10 阈值照抄）。结果缓存，重复调用直接返回。
func (s *Solver) Solve() *Solution {
	if s.solved {
		return s.solution
	}

	s.buildStateList()
	N := len(s.states)

	// 容许决策列表
	allowed := make([][]Action, N)
	for i := range s.states {
		allowed[i] = s.allowedActions(s.states[i])
	}

	// 1. 构建反向邻接表 reverseEdge[v] = { u : u 经某决策可到达 v }
	reverseEdge := make([][]int, N)
	for u := 0; u < N; u++ {
		for _, action := range allowed[u] {
			for _, t := range s.transitions(s.states[u], action) {
				v := s.index[mdpStateKey(t.state)]
				reverseEdge[v] = append(reverseEdge[v], u)
			}
		}
	}

	// 2. 反向 BFS 求最长步数（拓扑序依据）：从吸收态出发，取更长路径松弛。
	dist := make([]int, N)
	for i := range dist {
		dist[i] = -1
	}
	endIdx := s.index["END"] // 恒为 0
	dist[endIdx] = 0
	queue := []int{endIdx}
	for len(queue) > 0 {
		v := queue[0]
		queue = queue[1:]
		for _, u := range reverseEdge[v] {
			if dist[u] < dist[v]+1 { // 严格更长 = 最长路径松弛
				dist[u] = dist[v] + 1
				queue = append(queue, u)
			}
		}
	}

	// 3. 按 dist 升序（稳定）排序所有状态下标 → order
	order := make([]int, N)
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool {
		return dist[order[a]] < dist[order[b]]
	})

	// 4. DP：距吸收态近的先算 → 处理某状态时其所有后继价值已就绪
	value := make([]float64, N)
	policy := make([]Action, N)
	for _, idx := range order {
		st := s.states[idx]
		if st.isEnd {
			value[idx] = 0
			policy[idx] = ActionNone
			continue
		}
		bestVal := math.Inf(-1)
		bestAct := ActionNone
		for _, action := range allowed[idx] {
			imm := float64(s.immediateReward(st, action))
			fut := 0.0
			for _, t := range s.transitions(st, action) {
				fut += t.prob * value[s.index[mdpStateKey(t.state)]]
			}
			total := imm + fut
			// 1e-10 阈值：只有严格更优（超出浮点噪声）才更新最优决策
			if total > bestVal+1e-10 {
				bestVal = total
				bestAct = action
			}
		}
		value[idx] = bestVal
		policy[idx] = bestAct
	}

	// 组装对外 Solution（States：吸收态为零值 State，恒在 [0]）
	statesOut := make([]State, N)
	for i, ms := range s.states {
		statesOut[i] = ms.State
	}

	s.solution = &Solution{
		Value:  value,
		Policy: policy,
		States: statesOut,
		Index:  s.index,
	}
	s.solved = true
	return s.solution
}

// queryIndex 把对外查询 State 解析为状态空间下标。
// RemainCalc==0（吸收态）返回 END 的下标（恒为 0）；不可达状态返回 -1。
func (s *Solver) queryIndex(st State) int {
	if !s.solved {
		s.Solve()
	}
	if st.RemainCalc == 0 {
		return s.index["END"]
	}
	idx, ok := s.index[mdpStateKey(mdpState{State: st})]
	if !ok {
		return -1
	}
	return idx
}

// Decide 返回当前查询状态所有合法决策的评估结果，并标记与最优策略一致的那一项 IsBest。
//
// 不可达状态（手牌张数超牌库、违反 §6.8 过滤、或 RemainCalc==0）返回空切片。
func (s *Solver) Decide(st State) []Outcome {
	idx := s.queryIndex(st)
	if idx < 0 {
		return nil
	}
	from := s.getState(st.RemainCalc, st.RemainAband, st.RemainDouble, st.IsDoubled, st.Hand)
	if from.isEnd {
		return nil // 吸收态（RemainCalc==0）无决策
	}
	sol := s.solution
	best := sol.Policy[idx]

	allowed := s.allowedActions(from)
	outcomes := make([]Outcome, 0, len(allowed))
	for _, action := range allowed {
		imm := s.immediateReward(from, action)
		fut := 0.0
		for _, t := range s.transitions(from, action) {
			fut += t.prob * sol.Value[s.index[mdpStateKey(t.state)]]
		}
		outcomes = append(outcomes, Outcome{
			Action:    action,
			Immediate: imm,
			Expected:  fut,
			Total:     float64(imm) + fut,
			IsBest:    action == best,
		})
	}
	return outcomes
}

// Best 返回当前查询状态的最优决策（与最优策略一致）。
// 不可达状态返回 ActionNone 与非 nil error，调用方据此报错/重识别。
func (s *Solver) Best(st State) (Action, error) {
	idx := s.queryIndex(st)
	if idx < 0 {
		return ActionNone, fmt.Errorf("state is unreachable: %+v", st)
	}
	best := s.solution.Policy[idx]
	if best == ActionNone {
		return ActionNone, errors.New("no best action for absorbing state")
	}
	return best, nil
}

// Value 返回当前查询状态的期望总奖励（价值函数值）。
// 不可达状态返回 0, false。
func (s *Solver) Value(st State) (float64, bool) {
	idx := s.queryIndex(st)
	if idx < 0 {
		return 0, false
	}
	return s.solution.Value[idx], true
}

// ConfigKey 返回 Config 的稳定哈希键，用于 L1 层按配置缓存求解器实例
// （Config 变化才需要重新 NewSolver+Solve）。Deck/Reward 用值，OverflowMode 用标签。
func ConfigKey(cfg Config) string {
	return fmt.Sprintf("%v|%v|%d|%s", cfg.Deck, cfg.Reward, cfg.MaxDouble, cfg.OverflowMode)
}
