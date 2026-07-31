package strategy

type constructor func(Config) (Selector, bool)

var constructors = map[Kind]constructor{
	KindRarity: func(Config) (Selector, bool) {
		return Rarity{}, true
	},
	KindPrice: func(Config) (Selector, bool) {
		return Price{}, true
	},
	KindStock: func(config Config) (Selector, bool) {
		if config.MinimumUnitPrice < 0 {
			return nil, false
		}
		return Stock{MinimumUnitPrice: config.MinimumUnitPrice}, true
	},
}

// New 根据策略类型和配置创建 Selector。
func New(kind Kind, config Config) (Selector, bool) {
	build, ok := constructors[kind]
	if !ok {
		return nil, false
	}
	return build(config)
}
