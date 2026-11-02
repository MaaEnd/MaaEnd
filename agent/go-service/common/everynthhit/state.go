package everynthhit

import "sync"

// states 保存各 key 已累计的评估次数，由包级锁保护。
var states = struct {
	sync.Mutex
	counts map[string]int
}{counts: make(map[string]int)}

// Advance 记录 key 被评估一次，累计满 every 次时返回 true 并清零。
//
// 一次 Advance = 该识别节点被评估一次：命中后从 1 重新计数，未命中则继续累加。
// 因此同一轮里被重复评估的节点会消耗多次计数，Pipeline 侧应保证该节点每轮只被
// 评估一次（例如挂在识别命中后即离开的 next 链路上）。
// every <= 0 时既不计数也不命中，便于 Pipeline 直接关掉该行为。
func Advance(key string, every int) bool {
	if every <= 0 {
		return false
	}

	states.Lock()
	defer states.Unlock()

	states.counts[key]++
	if states.counts[key] < every {
		return false
	}
	delete(states.counts, key)
	return true
}

// Reset 清零 key 的计数，下一次 Advance 重新从 1 开始计数。
func Reset(key string) {
	states.Lock()
	delete(states.counts, key)
	states.Unlock()
}

// ResetAll 清零所有 key 的计数。
func ResetAll() {
	states.Lock()
	clear(states.counts)
	states.Unlock()
}
