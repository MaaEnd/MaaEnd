package trialofswordmancy

import (
	"testing"
	"time"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/trialofswordmancy/solver"
)

// 异步预求解端到端：冷缓存 spawn 后 await 拿到可用求解器；同配置二次预热不再 spawn
// （nil future = Decide 走 solverFor 直接命中）。
func TestPreSolveAwait(t *testing.T) {
	cfg := solver.DefaultConfig

	sess.SetPendingSolve(preSolveIfNeeded(cfg))
	slv, ok := awaitPreSolve(cfg)
	if !ok || slv == nil {
		t.Fatalf("awaitPreSolve failed: slv=%v ok=%v", slv, ok)
	}
	outcomes := slv.Decide(solver.State{
		RemainCalc:   2,
		RemainAband:  1,
		RemainDouble: 1,
		Hand:         [5]int{1, 0, 1, 0, 0},
	})
	if len(outcomes) == 0 {
		t.Fatal("reachable state should yield non-empty outcomes")
	}

	sess.SetPendingSolve(preSolveIfNeeded(cfg))
	if sess.PendingSolve() != nil {
		t.Fatal("warm cache should not spawn another pre-solve")
	}
	if slv, ok = awaitPreSolve(cfg); !ok || slv == nil {
		t.Fatalf("awaitPreSolve on warm cache failed: slv=%v ok=%v", slv, ok)
	}
}

// 同一轮次有多个 Decide 步：首个 await 消费 future 后必须清除，后续 await 直接走缓存，
// 不能阻塞在已被抽干的通道上（回归：第二次 Decide 无限等待）。
func TestPreSolveAwaitOncePerRound(t *testing.T) {
	cfg := solver.DefaultConfig
	cfg.Deck = [5]int{4, 6, 6, 6, 7} // 与其余用例不同的冷键

	sess.SetPendingSolve(preSolveIfNeeded(cfg))
	slv, ok := awaitPreSolve(cfg)
	if !ok || slv == nil {
		t.Fatalf("first await failed: slv=%v ok=%v", slv, ok)
	}

	type result struct {
		slv *solver.Solver
		ok  bool
	}
	ch := make(chan result, 1)
	go func() {
		slv, ok := awaitPreSolve(cfg)
		ch <- result{slv, ok}
	}()
	select {
	case r := <-ch:
		if !r.ok || r.slv == nil {
			t.Fatalf("second await failed: slv=%v ok=%v", r.slv, r.ok)
		}
		if r.slv != slv {
			t.Fatal("second await should hit the cache (same solver instance)")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("second await blocked: future must be consumed once per round")
	}
}

// 并发预热互不干扰：-race 下验证 solverCache 锁的正确性。
// 只走 preSolveIfNeeded + future 接收，不碰 sess（Session 依赖回调线程单线程，无锁）。
func TestPreSolveConcurrent(t *testing.T) {
	var cfgs []solver.Config
	for i := 0; i < 4; i++ {
		cfg := solver.DefaultConfig
		cfg.Deck = [5]int{5 + i, 5, 6, 6, 7}
		cfgs = append(cfgs, cfg)
	}
	done := make(chan struct{}, len(cfgs))
	for _, cfg := range cfgs {
		cfg := cfg
		go func() {
			defer func() { done <- struct{}{} }()
			f := preSolveIfNeeded(cfg)
			if f == nil {
				t.Errorf("expected spawn for cold cache %v", cfg.Deck)
				return
			}
			res := <-f.done
			if res.err != nil || res.slv == nil {
				t.Errorf("pre-solve failed for %v: %v", cfg.Deck, res.err)
			}
		}()
	}
	for range cfgs {
		<-done
	}
}
