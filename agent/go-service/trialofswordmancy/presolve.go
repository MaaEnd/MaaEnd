package trialofswordmancy

import (
	"fmt"
	"sync"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/trialofswordmancy/solver"
	"github.com/rs/zerolog/log"
)

// 本任务运行时信息由 recognition 从截图识别（手牌/牌库/剩余次数/翻倍态），
// reward/maxDouble 为等级 4 常量（solver.DefaultConfig）。
// overflowMode 是玩家策略选项，唯一数据源是 Decide 节点的 custom_action_param
// （见 decideOverflowMode）：RecognizeDeck 用它构造异步预求解的 cfg，Decide 用它覆盖
// recognition 的默认值——两侧同源，预求解配置与决策配置必然一致。

// —— solver 备忘：进程级、按 Config 哈希键，Config 变化才重新 Solve ——
// 与 Session（run 级状态，随轮次重置）不同：备忘不随轮次失效，只有上限兜底——正常一副牌 + 三种
// 溢出模式只产生极少量键；上限防牌库 OCR 抖动产生大量误识别键导致常驻内存增长。
// 求解由后台 goroutine 预热、回调线程取用，两侧都写这张表，必须加锁——MaaFramework 的
// 单线程保证只覆盖任务回调，不覆盖 goroutine。
const solverCacheLimit = 16

var (
	solverMu    sync.Mutex
	solverCache = map[string]*solver.Solver{}
)

// solverFor 返回给定配置的 *solver.Solver；缓存未命中（新牌库/新溢出模式）才构造求解。
// 异步预求解流程下回调线程只会带着「预热时缓存已热」的 cfg 走到这里（见 awaitPreSolve），
// miss 分支是纯防御路径：即使走到，也与改造前的同步求解行为一致。
func solverFor(cfg solver.Config) *solver.Solver {
	key := solver.ConfigKey(cfg)
	solverMu.Lock()
	defer solverMu.Unlock()
	if s, ok := solverCache[key]; ok {
		return s
	}
	if len(solverCache) >= solverCacheLimit {
		solverCache = make(map[string]*solver.Solver) // 超阈值清空，避免无限增长
	}
	s := solver.NewSolver(cfg)
	s.Solve()
	solverCache[key] = s
	return s
}

// solveFuture 是一次异步预求解的结果载体；done 为 buffered(1)，goroutine send 永不阻塞、必然退出。
type solveFuture struct {
	done chan solveResult
}

type solveResult struct {
	slv *solver.Solver
	err error
}

// preSolveIfNeeded 为 cfg 启动异步预求解；缓存已热返回 nil（本轮 Decide 无需等待，直接命中）。
// 锁内完成「查缓存 + 启动」，与 goroutine 的写缓存互斥——即使存在在途求解（stop/restart 边界），
// 最坏也只是同 key 双算一次，结果相同，无正确性影响。
func preSolveIfNeeded(cfg solver.Config) *solveFuture {
	solverMu.Lock()
	defer solverMu.Unlock()
	if _, ok := solverCache[solver.ConfigKey(cfg)]; ok {
		return nil
	}
	return spawnPreSolve(cfg)
}

// spawnPreSolve 在后台 goroutine 中求解并预热缓存。
// goroutine 只碰 solverCache（锁内）与 done——不碰 ctx 与 sess：MaaFW 的线程亲和只在任务
// 回调侧，跨线程调 ctx 方法或读写无锁 Session 都是未定义行为。
func spawnPreSolve(cfg solver.Config) *solveFuture {
	f := &solveFuture{done: make(chan solveResult, 1)}
	go func() {
		slv, err := solveOnce(cfg)
		if err != nil {
			log.Error().Err(err).Str("component", component).Msg("async pre-solve failed")
		}
		f.done <- solveResult{slv: slv, err: err}
	}()
	return f
}

// solveOnce 执行一次全量求解并写入缓存。
// panic 必须就地兜底为错误返回：goroutine 里未 recover 的 panic 会崩掉整个 go-service 进程；
// 锁内段用 defer 解锁，panic 也不会把锁留在持锁态。求解是纯算术（状态空间有界、除零有守卫），
// 失败即系统性 bug，由调用方（Decide）中止任务，不走兜底路径。
func solveOnce(cfg solver.Config) (slv *solver.Solver, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("pre-solve panicked: %v", r)
		}
	}()
	s := solver.NewSolver(cfg)
	s.Solve()
	solverMu.Lock()
	defer solverMu.Unlock()
	if len(solverCache) >= solverCacheLimit {
		solverCache = make(map[string]*solver.Solver)
	}
	solverCache[solver.ConfigKey(cfg)] = s
	return s, nil
}

// awaitPreSolve 取用本轮异步预求解的求解器，供首个 Decide 使用。
// future 非 nil 时阻塞等 goroutine 完成：求解早在轮次开始时启动，常态瞬间返回；最坏情况
// （预热后立即决策）等价于改造前的同步求解，只是阻塞位置挪到了这里。future 为 nil 说明
// 预热时缓存已热或本轮已消费，solverFor 必命中。
// 取用后即清掉 Session 里的 future：一轮里有多个 Decide 步，只有第一个需要等 goroutine——
// goroutine 先写缓存再发信号，消费完成即缓存已热，后续 Decide 直接走 solverFor；不清理的话
// 第二个 Decide 会阻塞在已被抽干的通道上。
func awaitPreSolve(cfg solver.Config) (*solver.Solver, bool) {
	f := sess.PendingSolve()
	if f == nil {
		return solverFor(cfg), true
	}
	res := <-f.done
	sess.SetPendingSolve(nil)
	if res.err != nil {
		return nil, false
	}
	return res.slv, true
}
