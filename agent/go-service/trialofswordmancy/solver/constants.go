package solver

import "time"

// 等级固定为 4 的两组常量（§5.2 / §5.3）。

// DefaultDeck 是等级 4 的默认牌库：点数 1,2,3,4,5 的库存分别为 4,5,6,6,7。
//
// 注意牌库会随刷新周期变化，这里只是默认值（§5.1）。
var DefaultDeck = [5]int{4, 5, 6, 6, 7}

// DefaultReward 是等级 4 的演算奖励元组（长度 11，下标 0..10 = 战力点）。
var DefaultReward = [11]int{0, 1000, 2000, 4000, 7500, 12000, 20000, 36000, 60000, 100000, 160000}

// MaxDouble 是等级 4 的翻倍次数上限，恒为 2。
const MaxDouble = 2

// DefaultConfig 是等级 4 的默认基础设定：默认牌库 + 等级 4 奖励 + 翻倍上限 2 +
// 默认溢出模式「接受1至2次」（§5.5）。
var DefaultConfig = Config{
	Deck:         DefaultDeck,
	Reward:       DefaultReward,
	MaxDouble:    MaxDouble,
	OverflowMode: OverflowTwice,
}

// DefaultState 是默认查询状态：剩余演算 3 / 放弃 3 / 翻倍 2 / 未翻倍 / 空手（§5.5）。
var DefaultState = State{
	RemainCalc:   3,
	RemainAband:  3,
	RemainDouble: 2,
	IsDoubled:    false,
	Hand:         [5]int{0, 0, 0, 0, 0},
}

// 牌库刷新周期（§5.4，来自 trialOfSwordmancyDeck.ts）。
//
// 月份陷阱：TypeScript 源码 Date.UTC(2026, 5, 8, 20, 0, 0) 的月份是 0-indexed，
// 即 6 月；Go 的 time.Date 月份是 1-indexed，必须写 6。差一个月会让刷新周期算错。
var (
	DeckRefreshOrigin = time.Date(2026, 6, 8, 20, 0, 0, 0, time.UTC)
	DeckRefreshCycle  = 72 * time.Hour
)

// RefreshCycleNumber 返回 now 所处的牌库刷新周期编号；
// now 早于刷新起点时返回 -1（周期尚未开始）。
func RefreshCycleNumber(now time.Time) int {
	if now.Before(DeckRefreshOrigin) {
		return -1
	}
	return int(now.Sub(DeckRefreshOrigin) / DeckRefreshCycle)
}
