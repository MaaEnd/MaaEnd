// Package solver 实现选剑演武（Trial of Swordmancy）的精确 MDP 求解器。
//
// 它是 1:1 从前端 TypeScript 实现（ef-frontend-v1/shared/utils/trialOfSwordmancy.ts
// 的 求解器类）迁移而来的纯算法包，零 MaaEnd 依赖，可独立 go test 与复用。
//
// 算法：有限视野马尔可夫决策过程，用「反向 BFS 求 DAG 最长路得到拓扑序」+「按
// 拓扑序做 Bellman 最优 DP」精确求解，无任何蒙特卡洛/贪心/启发式近似。
package solver

import (
	"encoding/json"
	"fmt"
)

// Action 是选剑演武的单步决策枚举。
//
// 零值 ActionNone 表示「无决策」，用于吸收态（演算次数耗尽）的最优策略占位，
// 与 TypeScript 源码里 最优策略 吸收态为 null 对应。
type Action int

const (
	ActionNone Action = iota // 无决策（吸收态占位，零值；不应作为查询结果返回）
	DrawCard                 // 抽取铭牌
	Abandon                  // 放弃
	Calculate                // 开始演算
	Double                   // 选择翻倍
)

// String 返回决策的可读标签，用于日志与 detail JSON。
// 注意与 TypeScript 源码的字符串值（抽取铭牌/放弃/开始演算/选择翻倍）一一对应。
func (a Action) String() string {
	switch a {
	case DrawCard:
		return "DrawCard"
	case Abandon:
		return "Abandon"
	case Calculate:
		return "Calculate"
	case Double:
		return "Double"
	default:
		return "None"
	}
}

// MarshalJSON 把 Action 序列化为可读字符串（detail JSON 硬契约 §7.3 / 日志友好）。
func (a Action) MarshalJSON() ([]byte, error) {
	return json.Marshal(a.String())
}

// UnmarshalJSON 从字符串还原 Action。
func (a *Action) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	switch s {
	case "DrawCard":
		*a = DrawCard
	case "Abandon":
		*a = Abandon
	case "Calculate":
		*a = Calculate
	case "Double":
		*a = Double
	case "None":
		*a = ActionNone
	default:
		return fmt.Errorf("invalid Action string: %q", s)
	}
	return nil
}

// OverflowMode 是「数据溢出」处理模式，决定总点数爆表时奖励是否归零。
type OverflowMode int

const (
	OverflowNone  OverflowMode = iota // 不接受：总点数≥11 归零
	OverflowOnce                      // 接受1次：总点数≥22 归零
	OverflowTwice                     // 接受1至2次：无上限（默认）
)

// String 返回溢出模式标签，与 detail JSON（§7.3）一致。
func (m OverflowMode) String() string {
	switch m {
	case OverflowNone:
		return "OverflowNone"
	case OverflowOnce:
		return "OverflowOnce"
	case OverflowTwice:
		return "OverflowTwice"
	default:
		return "OverflowTwice"
	}
}

// MarshalJSON 把 OverflowMode 序列化为字符串（detail JSON 硬契约 §7.3）。
func (m OverflowMode) MarshalJSON() ([]byte, error) {
	return json.Marshal(m.String())
}

// UnmarshalJSON 从字符串还原 OverflowMode；无法识别的值返回 error（由调用方决定回退，如 loadOverflowMode 回退 OverflowNone）。
func (m *OverflowMode) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	switch s {
	case "OverflowNone":
		*m = OverflowNone
	case "OverflowOnce":
		*m = OverflowOnce
	case "OverflowTwice":
		*m = OverflowTwice
	default:
		return fmt.Errorf("invalid OverflowMode string: %q", s)
	}
	return nil
}

// Config 是决定整棵 MDP 的基础设定。任一字段变化都需要重新 Solve。
//
// 等级固定为 4：Reward 即等级 4 的奖励表、MaxDouble 恒为 2；
// 实际会变的只有 Deck（牌库随刷新周期变）与 OverflowMode（随用户选项变）。
type Config struct {
	Deck         [5]int       `json:"deck"`      // 各点数(1..5)牌库存；默认 [4,5,6,6,7]
	Reward       [11]int      `json:"reward"`    // 战力点 0..10 → 奖励；等级 4 常量
	MaxDouble    int          `json:"maxDouble"` // 翻倍次数上限；固定 2
	OverflowMode OverflowMode `json:"overflowMode"`
}

// State 是对外查询的当前游戏状态（每步循环都变）。
// 全部为扁平字段，Hand 用 [5]int 值类型避免底层数组串值。
type State struct {
	RemainCalc   int    `json:"remainCalc"`   // 剩余演算次数 1..3（0 = 已结束/吸收态）
	RemainAband  int    `json:"remainAband"`  // 剩余放弃次数 0..3
	RemainDouble int    `json:"remainDouble"` // 剩余翻倍次数 0..MaxDouble
	IsDoubled    bool   `json:"isDoubled"`    // 本局是否已选择翻倍
	Hand         [5]int `json:"hand"`         // 手牌各点数张数（下标 0 = 点数 1）
}

// Outcome 是单个决策的评估结果。
type Outcome struct {
	Action    Action  `json:"action"`
	Immediate int     `json:"immediate"` // 即时奖励
	Expected  float64 `json:"expected"`  // 期望未来价值
	Total     float64 `json:"total"`     // 总价值 = Immediate + Expected
	IsBest    bool    `json:"isBest"`    // 是否最优（与最优策略一致）
}

// Solution 是全量求解产物：每个状态的期望总奖励（价值函数）与最优决策（策略），
// 外加状态列表与状态键索引。对外查询走 Decide / Best，通常不必直接读 Solution。
type Solution struct {
	Value  []float64      // 价值函数，下标对应 States
	Policy []Action       // 最优策略，下标对应 States；吸收态为 ActionNone
	States []State        // 状态列表，吸收态恒为 States[0]（零值 State）
	Index  map[string]int // 状态键 → 下标；"END" → 0
}
