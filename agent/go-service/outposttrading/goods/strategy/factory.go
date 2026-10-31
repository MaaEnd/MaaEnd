package strategy

var constructors = map[Kind]func() Selector{
	KindRarity: func() Selector { return Rarity{} },
	KindPrice:  func() Selector { return Price{} },
	KindStock:  func() Selector { return Stock{} },
}

// New 根据策略类型创建 Selector。
func New(kind Kind) (Selector, bool) {
	build, ok := constructors[kind]
	if !ok {
		return nil, false
	}
	return build(), true
}
