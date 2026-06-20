package trialofswordmancy

import (
	"sync"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/trialofswordmancy/solver"
)

// 本任务所有运行时信息都由 recognition 从截图识别（手牌/牌库/剩余次数/翻倍态），
// overflowMode 硬编码为 OverflowTwice、reward/maxDouble 为等级 4 常量（solver.DefaultConfig）。
// Decide 节点不带 custom_action_param，因此这里不再从节点加载配置。

// —— 求解器缓存：按 Config 哈希键，Config 变化才重新 Solve ——
var (
	solverCacheMu sync.Mutex
	solverCache   = map[string]*solver.Solver{}
)

// solverFor 返回（必要时构造并预求解）给定配置的 *solver.Solver。
// 每步查询复用同一实例，仅 Config 变化（牌库刷新 / 溢出模式变化）时才重 Solve。
func solverFor(cfg solver.Config) *solver.Solver {
	key := solver.ConfigKey(cfg)
	solverCacheMu.Lock()
	defer solverCacheMu.Unlock()
	if s, ok := solverCache[key]; ok {
		return s
	}
	s := solver.NewSolver(cfg)
	s.Solve() // 预求解并缓存
	solverCache[key] = s
	return s
}
