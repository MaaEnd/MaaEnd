package solver

// 等级固定为 4 的两组常量（§5.2 / §5.3）。

// DefaultDeck 是等级 4 的默认牌库：点数 1,2,3,4,5 的库存分别为 4,5,6,6,7。
//
// 注意牌库会随刷新周期变化，这里只是默认值（§5.1）。
var DefaultDeck = [5]int{4, 5, 6, 6, 7}

// DefaultReward 是等级 4 的演算奖励元组（长度 11，下标 0..10 = 战力点）。
var DefaultReward = [11]int{0, 1000, 2000, 4000, 7500, 12000, 20000, 36000, 60000, 100000, 160000}

// MaxDouble 是等级 4 的翻倍次数上限，恒为 2。
const MaxDouble = 2

// maxRemainCalc 是包含跨日残局时的最大剩余演算次数。
const maxRemainCalc = 4

// DefaultConfig 是等级 4 的默认基础设定：默认牌库 + 等级 4 奖励 + 翻倍上限 2 +
// 默认溢出模式「接受1至2次」（§5.5）。
var DefaultConfig = Config{
	Deck:         DefaultDeck,
	Reward:       DefaultReward,
	MaxDouble:    MaxDouble,
	OverflowMode: OverflowTwice,
}
