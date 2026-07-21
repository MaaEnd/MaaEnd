package trialofswordmancy

import (
	"github.com/MaaXYZ/MaaEnd/agent/go-service/trialofswordmancy/solver"
)

// GameState 是 recognition 经 RecognitionDetail.Detail 传给 Decide 动作的 JSON 载体：
// recognition 组装后 Marshal 进 Detail，Decide Unmarshal 读回。
//
// State 与 Config 的字段主要由 recognition 从截图识别得出（reward/maxDouble 为等级 4 常量）；
// overflowMode 在 recognition 中为默认值，最终由 Decide 节点的 custom_action_param.overflowMode 覆盖。
type GameState struct {
	State  solver.State  `json:"state"`
	Config solver.Config `json:"config"`

	// 以下为识别原始字段，供日志/调试观测（写进 detail JSON），action 不读、不参与 MDP 求解。
	HandRaw  [5]int `json:"handRaw"`  // 各槽位识别到的点数（下标=槽位，0=空槽）
	Overflow bool   `json:"overflow"` // 是否识别到溢出叹号（爆表）
}
