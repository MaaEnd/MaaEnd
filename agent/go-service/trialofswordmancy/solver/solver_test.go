package solver

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// —— 纯函数断言（§10.1）——

func TestTotalScore(t *testing.T) {
	cases := []struct {
		hand [5]int
		want int
	}{
		{[5]int{0, 1, 0, 0, 0}, 2},   // 1 张 2 点
		{[5]int{0, 0, 0, 0, 1}, 5},   // 1 张 5 点
		{[5]int{1, 1, 1, 1, 1}, 15},  // 满手各一张
		{[5]int{0, 0, 0, 0, 0}, 0},   // 空手
		{[5]int{0, 0, 0, 0, 2}, 10},  // 2 张 5 点
		{[5]int{5, 0, 0, 0, 0}, 5},   // 5 张 1 点
	}
	for _, c := range cases {
		if got := TotalScore(c.hand); got != c.want {
			t.Errorf("TotalScore(%v) = %d, want %d", c.hand, got, c.want)
		}
	}
}

func TestPowerOf(t *testing.T) {
	cases := []struct {
		hand [5]int
		want int
	}{
		{[5]int{1, 1, 1, 1, 1}, 4},  // 15 % 11
		{[5]int{0, 0, 0, 0, 2}, 10}, // 10 % 11
		{[5]int{0, 0, 0, 0, 0}, 0},
		{[5]int{0, 0, 0, 0, 5}, 3},  // 25 % 11
	}
	for _, c := range cases {
		if got := PowerOf(c.hand); got != c.want {
			t.Errorf("PowerOf(%v) = %d, want %d", c.hand, got, c.want)
		}
	}
}

// calcSettleReward 覆盖三种溢出模式在 sum<11 / 11≤sum<22 / sum≥22 的归零 + 翻倍 ×2。
func TestCalcSettleReward(t *testing.T) {
	mk := func(mode OverflowMode) *Solver { return NewSolver(Config{Deck: DefaultDeck, Reward: DefaultReward, MaxDouble: MaxDouble, OverflowMode: mode}) }

	// sum=2 (power=2): 不归零；reward[2]=2000；翻倍 4000。三种模式都一致。
	for _, mode := range []OverflowMode{OverflowNone, OverflowOnce, OverflowTwice} {
		s := mk(mode)
		if got := s.calcSettleReward([5]int{0, 1, 0, 0, 0}, false); got != 2000 {
			t.Errorf("mode=%v sum=2 notDoubled: %d, want 2000", mode, got)
		}
		if got := s.calcSettleReward([5]int{0, 1, 0, 0, 0}, true); got != 4000 {
			t.Errorf("mode=%v sum=2 doubled: %d, want 4000", mode, got)
		}
	}

	// sum=15 (power=4, [0,0,0,0,3]): OverflowNone 归零；其余 reward[4]=7500。
	if got := mk(OverflowNone).calcSettleReward([5]int{0, 0, 0, 0, 3}, false); got != 0 {
		t.Errorf("OverflowNone sum=15: %d, want 0", got)
	}
	if got := mk(OverflowOnce).calcSettleReward([5]int{0, 0, 0, 0, 3}, false); got != 7500 {
		t.Errorf("OverflowOnce sum=15: %d, want 7500", got)
	}
	if got := mk(OverflowTwice).calcSettleReward([5]int{0, 0, 0, 0, 3}, false); got != 7500 {
		t.Errorf("OverflowTwice sum=15: %d, want 7500", got)
	}

	// sum=25 (power=3, [0,0,0,0,5]): OverflowNone/OverflowOnce 归零；Twice reward[3]=4000。
	if got := mk(OverflowOnce).calcSettleReward([5]int{0, 0, 0, 0, 5}, false); got != 0 {
		t.Errorf("OverflowOnce sum=25: %d, want 0", got)
	}
	if got := mk(OverflowTwice).calcSettleReward([5]int{0, 0, 0, 0, 5}, false); got != 4000 {
		t.Errorf("OverflowTwice sum=25: %d, want 4000", got)
	}
	if got := mk(OverflowTwice).calcSettleReward([5]int{0, 0, 0, 0, 5}, true); got != 8000 {
		t.Errorf("OverflowTwice sum=25 doubled: %d, want 8000", got)
	}
}

// —— 状态空间不变量（§10.2）——

func TestStateSpaceInvariants(t *testing.T) {
	s := NewSolver(DefaultConfig)
	sol := s.Solve()

	// 吸收态恒为 States[0] 且为零值 State
	if sol.Index["END"] != 0 {
		t.Errorf("Index[END] = %d, want 0", sol.Index["END"])
	}
	if (sol.States[0] != State{}) {
		t.Errorf("States[0] = %+v, want zero State (absorbing)", sol.States[0])
	}
	if len(sol.Value) != len(sol.States) || len(sol.Policy) != len(sol.States) {
		t.Errorf("Value/Policy length mismatch: states=%d value=%d policy=%d", len(sol.States), len(sol.Value), len(sol.Policy))
	}

	// 所有非吸收态满足 stateFilter == true，且「已用翻倍 ≤ 已用演算」成立。
	for i, st := range sol.States {
		if i == 0 {
			continue
		}
		if !s.stateFilter(st) {
			t.Errorf("transient state[%d] %+v fails stateFilter", i, st)
		}
		usedDouble := s.cfg.MaxDouble - st.RemainDouble
		usedCalc := 3 - st.RemainCalc
		if usedDouble > usedCalc {
			t.Errorf("state[%d] %+v: usedDouble %d > usedCalc %d", i, st, usedDouble, usedCalc)
		}
	}

	// 每个非吸收态至少有一条合法决策，且其最优策略不是 ActionNone
	for i, st := range sol.States {
		if i == 0 {
			continue
		}
		if len(s.allowedActions(mdpState{State: st})) == 0 {
			t.Errorf("state[%d] %+v has no allowed actions", i, st)
		}
		if sol.Policy[i] == ActionNone {
			t.Errorf("state[%d] %+v best policy is ActionNone", i, st)
		}
	}
}

func TestSolvePerformance(t *testing.T) {
	start := time.Now()
	sol := NewSolver(DefaultConfig).Solve()
	elapsed := time.Since(start)
	if elapsed > 200*time.Millisecond {
		t.Errorf("Solve took %v (states=%d), want < 200ms", elapsed, len(sol.States))
	}
	t.Logf("Solve: %d states in %v", len(sol.States), elapsed)
}

// —— Decide / Best 行为 ——

func TestDecideAndBest(t *testing.T) {
	s := NewSolver(DefaultConfig)
	s.Solve()

	// 默认查询态：应有非空 Decide，恰有一个 IsBest，Best 不报错。
	outcomes := s.Decide(DefaultState)
	if len(outcomes) == 0 {
		t.Fatalf("Decide(DefaultState) returned empty")
	}
	bestCount := 0
	for _, o := range outcomes {
		if o.IsBest {
			bestCount++
		}
		// Total == Immediate + Expected
		if math.Abs(o.Total-(float64(o.Immediate)+o.Expected)) > 1e-9 {
			t.Errorf("outcome %+v: Total != Immediate+Expected", o)
		}
	}
	if bestCount != 1 {
		t.Errorf("Decide(DefaultState) has %d IsBest, want exactly 1", bestCount)
	}

	best, err := s.Best(DefaultState)
	if err != nil {
		t.Fatalf("Best(DefaultState) error: %v", err)
	}
	if best == ActionNone {
		t.Errorf("Best(DefaultState) = ActionNone, want a real action")
	}

	// IsBest 的那一项应与 Best 一致
	var marked Action
	for _, o := range outcomes {
		if o.IsBest {
			marked = o.Action
		}
	}
	if marked != best {
		t.Errorf("Decide IsBest=%s but Best=%s", marked, best)
	}

	// 满手牌：只允许 Calculate / Abandon
	full := State{RemainCalc: 3, RemainAband: 3, RemainDouble: 2, Hand: [5]int{1, 1, 1, 1, 1}}
	fullOutcomes := s.Decide(full)
	for _, o := range fullOutcomes {
		if o.Action != Calculate && o.Action != Abandon {
			t.Errorf("full hand allows %s, want only Calculate/Abandon", o.Action)
		}
	}

	// 不可达状态：Best 报错、Decide 返回空
	unreachable := State{RemainCalc: 3, RemainAband: 3, RemainDouble: 2, IsDoubled: true, Hand: [5]int{1, 0, 0, 0, 0}}
	if _, err := s.Best(unreachable); err == nil {
		t.Errorf("Best(unreachable) expected error, got nil")
	}
	if out := s.Decide(unreachable); len(out) != 0 {
		t.Errorf("Decide(unreachable) = %v, want empty", out)
	}
}

func TestRefreshCycleNumber(t *testing.T) {
	// 起点之前 → -1
	before := DeckRefreshOrigin.Add(-time.Hour)
	if n := RefreshCycleNumber(before); n != -1 {
		t.Errorf("RefreshCycleNumber(before origin) = %d, want -1", n)
	}
	// 起点本身 → 0
	if n := RefreshCycleNumber(DeckRefreshOrigin); n != 0 {
		t.Errorf("RefreshCycleNumber(origin) = %d, want 0", n)
	}
	// 起点 + 1.5 周期 → 1
	mid := DeckRefreshOrigin.Add(time.Hour * 72 * 3 / 2)
	if n := RefreshCycleNumber(mid); n != 1 {
		t.Errorf("RefreshCycleNumber(1.5 cycles) = %d, want 1", n)
	}
}

// —— TS 交叉验证（§10.3）：golden.json 由 TS oracle 生成，逐值对齐 ——

func TestCrossValidationAgainstTSOracle(t *testing.T) {
	goldenPath := filepath.Join("testdata", "golden.json")
	data, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Skipf("golden.json not found (%v); generate it via the TS dump script first", err)
	}

	var golden goldenFile
	if err := json.Unmarshal(data, &golden); err != nil {
		t.Fatalf("failed to parse golden.json: %v", err)
	}
	if len(golden.Configs) == 0 {
		t.Fatal("golden.json has no configs")
	}

	const valueTol = 1e-9
	var checked, policyChecked int
	for _, gc := range golden.Configs {
		cfg := Config{
			Deck:         gc.Deck,
			Reward:       DefaultReward,
			MaxDouble:    MaxDouble,
			OverflowMode: parseOverflowMode(gc.OverflowMode),
		}
		sol := NewSolver(cfg).Solve()

		// 全状态价值函数 + 最优策略逐项对齐（按状态键）
		goldenStates := map[string]goldenState{}
		for _, gs := range gc.States {
			goldenStates[gs.Key] = gs
		}
		if len(goldenStates) != len(sol.States) {
			t.Errorf("config %s: state count mismatch golden=%d go=%d", gc.OverflowMode, len(goldenStates), len(sol.States))
		}

		for i, st := range sol.States {
			if i == 0 {
				// 吸收态：END
				if gs, ok := goldenStates["END"]; !ok || gs.Key != "END" {
					t.Errorf("config %s: absorbing state missing in golden", gc.OverflowMode)
				}
				continue
			}
			key := mdpStateKey(mdpState{State: st})
			gs, ok := goldenStates[key]
			if !ok {
				t.Errorf("config %s state[%d] key=%s not found in golden", gc.OverflowMode, i, key)
				continue
			}
			checked++
			// 价值差 < 1e-9
			if diff := math.Abs(gs.Value - sol.Value[i]); diff >= valueTol {
				t.Errorf("config %s key=%s: value diff %g (go=%g golden=%g)", gc.OverflowMode, key, diff, sol.Value[i], gs.Value)
			}
			// 最优决策完全一致
			policyChecked++
			if gs.Policy != sol.Policy[i].String() {
				t.Errorf("config %s key=%s: policy go=%s golden=%s", gc.OverflowMode, key, sol.Policy[i], gs.Policy)
			}
		}
	}
	if checked == 0 {
		t.Fatal("cross-validation checked zero states; golden data may be malformed")
	}
	t.Logf("cross-validation: %d state-values + %d policies compared across %d configs", checked, policyChecked, len(golden.Configs))
}

// golden 文件结构（与 TS dump 脚本输出一致）。
type goldenFile struct {
	Configs []goldenConfig `json:"configs"`
}

type goldenConfig struct {
	OverflowMode string        `json:"overflowMode"`
	Deck         [5]int        `json:"deck"`
	States       []goldenState `json:"states"`
	Queries      []goldenQuery `json:"queries"`
}

type goldenState struct {
	Key    string  `json:"key"`
	Value  float64 `json:"value"`
	Policy string  `json:"policy"`
}

type goldenQuery struct {
	State   [5]int        `json:"hand"` // 占位：查询态在 TS 侧用结构化字段描述
	Outcomes []goldenOutcome `json:"outcomes"`
}

type goldenOutcome struct {
	Action    string  `json:"action"`
	Immediate int     `json:"immediate"`
	Expected  float64 `json:"expected"`
	Total     float64 `json:"total"`
	IsBest    bool    `json:"isBest"`
}

func parseOverflowMode(s string) OverflowMode {
	switch strings.TrimSpace(s) {
	case "OverflowNone", "不接受":
		return OverflowNone
	case "OverflowOnce", "接受1次":
		return OverflowOnce
	default:
		return OverflowTwice
	}
}
