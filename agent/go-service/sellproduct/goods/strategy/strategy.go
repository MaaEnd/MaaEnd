// Package strategy 提供 SellProduct 的纯选品策略。
// 画面识别、会话状态和公共候选过滤仍由上层 sellproduct 包负责。
package strategy

// Kind 标识一种选品策略。
type Kind string

const (
	// KindRarity 按稀有度和单价选择。
	KindRarity Kind = "rarity"
	// KindPrice 按单价和稀有度选择。
	KindPrice Kind = "price"
	// KindStock 按库存量和单价选择。
	KindStock Kind = "stock"
)

// Config 包含各选品策略可使用的配置。
type Config struct {
	MinimumUnitPrice int
}

// Candidate 表示已经通过公共过滤、等待策略选择的第一页货品。
// 输入顺序仅用于业务字段完全相同时的稳定兜底。
type Candidate struct {
	ItemID     string
	Stock      int64
	StockKnown bool
	Rarity     int
	UnitPrice  int
}

// Selector 从候选中选择一个货品，不得修改输入切片。
type Selector interface {
	Select(candidates []Candidate) (Candidate, bool)
}

// Orderer 是能够为运行时计划生成完整静态顺序的 Selector。
type Orderer interface {
	Selector
	Sort(candidates []Candidate) []Candidate
}
