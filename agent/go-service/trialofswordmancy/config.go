package trialofswordmancy

import (
	"sync"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/trialofswordmancy/solver"
)

// 本任务运行时信息由 recognition 从截图识别（手牌/牌库/剩余次数/翻倍态），
// reward/maxDouble 为等级 4 常量（solver.DefaultConfig）。
// overflowMode 是玩家策略选项：recognition 用默认值，最终由 Decide 节点的
// custom_action_param.overflowMode 覆盖（见 DecideAction.Run / loadOverflowMode）。

// —— 求解器缓存：按 Config 哈希键，Config 变化才重新 Solve ——
// 正常一副牌 + 三种溢出模式只产生极少量键；上限兜底，防止牌库 OCR 抖动产生大量误识别键导致常驻内存增长。
const solverCacheLimit = 16

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
	if len(solverCache) >= solverCacheLimit {
		solverCache = make(map[string]*solver.Solver) // 超阈值清空，避免无限增长
	}
	s := solver.NewSolver(cfg)
	s.Solve() // 预求解并缓存
	solverCache[key] = s
	return s
}
