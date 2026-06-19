package trialofswordmancy

import (
	"github.com/MaaXYZ/MaaEnd/agent/go-service/trialofswordmancy/solver"
)

// GameState 是 Custom Recognition 经 RecognitionDetail.Detail 传给 Custom Action 的
// 硬契约（迁移文档 §7.3）。State 字段每步来自识别 + 状态跟踪；Config 字段来自节点参数。
type GameState struct {
	State  solver.State  `json:"state"`
	Config solver.Config `json:"config"`

	// 以下为识别原始字段，供日志/UI 观测使用，不参与 MDP 求解。
	HandRaw      [5]int `json:"handRaw"`     // 各槽位识别到的点数（下标=槽位，0 表示空槽）
	OnCardScreen bool   `json:"onCardScreen"`// 是否处于奖励演算（抽牌）界面
	Overflow     bool   `json:"overflow"`    // 是否识别到溢出叹号（爆表）
}

// actionDecision 是 Decide 动作执行后留在包内（仅日志用）的决策摘要。
type actionDecision struct {
	Action solver.Action
	State  solver.State
}
