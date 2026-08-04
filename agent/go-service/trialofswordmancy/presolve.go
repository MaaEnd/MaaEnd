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
// 与 RoundState（随轮次重置）相反：备忘不随轮次失效，只有上限兜底——正常一副牌 + 三种
// 溢出模式只产生极少量键，上限防牌库 OCR 抖动产生大量误识别键常驻内存。
// 必须加锁：预求解 goroutine 与回调线程两侧都写这张表，MaaFramework 的单线程保证
// 只覆盖任务回调，不覆盖 goroutine。
const solverCacheLimit = 16

// cacheEntry 是备忘的一个条目，二态：已求解（slv）/ 在途求解（future）。
// 在途条目让同 key 预热复用同一 future，避免重复 spawn（同 key 双算）。
type cacheEntry struct {
	slv    *solver.Solver
	future *solveFuture
}

var (
	solverMu    sync.Mutex
	solverCache = map[string]*cacheEntry{}
)

// solverFor 返回配置对应的求解器；未命中才构造求解。异步预热下回调线程只会带
// 「已热」的 cfg 到这里（awaitPreSolve），miss 分支纯防御，行为与改造前同步求解一致。
func solverFor(cfg solver.Config) *solver.Solver {
	key := solver.ConfigKey(cfg)
	solverMu.Lock()
	defer solverMu.Unlock()
	if e, ok := solverCache[key]; ok && e.slv != nil {
		return e.slv
	}
	if len(solverCache) >= solverCacheLimit {
		solverCache = make(map[string]*cacheEntry) // 超阈值清空，避免无限增长
	}
	s := solver.NewSolver(cfg)
	s.Solve()
	solverCache[key] = &cacheEntry{slv: s}
	return s
}

// solveFuture 是一次异步预求解的结果载体。done 用 close 广播而非 send：
// 跨轮同 key 在途时可能不止一个 Decide 等在同一个 future 上，close 让所有等待方
// 同时醒来（close 同时建立 err 的 happens-before）。
type solveFuture struct {
	done chan struct{}
	err  error
}

// preSolveIfNeeded 为 cfg 启动异步预求解；缓存已热返回 nil（Decide 无需等待）。
// 在途时复用既有 future，不重复 spawn；锁内完成检查+标记+启动，与 goroutine 写缓存互斥。
func preSolveIfNeeded(cfg solver.Config) *solveFuture {
	key := solver.ConfigKey(cfg)
	solverMu.Lock()
	defer solverMu.Unlock()
	if e, ok := solverCache[key]; ok && e.slv != nil {
		return nil
	}
	if e, ok := solverCache[key]; ok && e.future != nil {
		return e.future
	}
	f := &solveFuture{done: make(chan struct{})}
	solverCache[key] = &cacheEntry{future: f}
	go func() {
		f.err = solveOnce(cfg)
		if f.err != nil {
			log.Error().Err(f.err).Str("component", component).Msg("async pre-solve failed")
		}
		close(f.done)
	}()
	return f
}

// solveOnce 全量求解并写入缓存（覆盖在途标记条目）。
// panic 就地兜底为错误：goroutine 里未 recover 的 panic 会崩掉整个 go-service 进程；
// 锁内段 defer 解锁，panic 也不会持锁。求解是纯算术（状态空间有界、除零有守卫），
// 失败即系统性 bug，由调用方（Decide）中止任务，不走兜底路径。
func solveOnce(cfg solver.Config) (err error) {
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
		solverCache = make(map[string]*cacheEntry)
	}
	solverCache[solver.ConfigKey(cfg)] = &cacheEntry{slv: s}
	return nil
}

// awaitPreSolve 取用 cfg 对应的求解器。在途时阻塞等 goroutine：求解在轮次开始时
// 就已启动，常态瞬间返回，最坏等价于改造前的同步求解。一轮有多个 Decide 步，
// 只有第一个可能等在途——goroutine 完成即写入缓存，后续直接命中，无需清理。
// 返回 false = 求解失败（系统性 bug），由调用方中止任务。
func awaitPreSolve(cfg solver.Config) (*solver.Solver, bool) {
	key := solver.ConfigKey(cfg)
	solverMu.Lock()
	e, ok := solverCache[key]
	if !ok || e.slv != nil {
		solverMu.Unlock()
		return solverFor(cfg), true
	}
	f := e.future
	solverMu.Unlock()

	<-f.done
	if f.err != nil {
		return nil, false
	}

	solverMu.Lock()
	e2, ok2 := solverCache[key]
	solverMu.Unlock()
	if ok2 && e2.slv != nil {
		return e2.slv, true
	}
	// 备忘被超上限清空等边缘：in-flight 结果已随条目丢失，防御性同步求解（与 solverFor miss 分支一致）。
	return solverFor(cfg), true
}
