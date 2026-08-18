// Package iconrecognition 提供 IconRecognition 的 Go 协议数据类型与解析辅助。
package iconrecognition

// ItemFilter 是 storageKind:categoryType 形式的候选过滤器。
type ItemFilter string

// NormalFilters 提供 Normal 存储下的合法分类过滤器。
type NormalFilters struct {
	Any            ItemFilter
	Ore            ItemFilter
	Plant          ItemFilter
	Product        ItemFilter
	Doodad         ItemFilter
	Nurturance     ItemFilter
	Usable         ItemFilter
	Producer       ItemFilter
	PortableDevice ItemFilter
}

// ValuableDepotFilters 提供 ValuableDepot 存储下的合法分类过滤器。
type ValuableDepotFilters struct {
	Any            ItemFilter
	CommercialItem ItemFilter
	SpecialItem    ItemFilter
	Weapon         ItemFilter
}

// IsolateFilters 提供 Isolate 存储下的合法分类过滤器。
type IsolateFilters struct {
	Any           ItemFilter
	AdventureExp  ItemFilter
	BPExp         ItemFilter
	Diamond       ItemFilter
	DomainGold    ItemFilter
	Gold          ItemFilter
	Originium     ItemFilter
	SpaceshipGold ItemFilter
	WeaponGold    ItemFilter
}

// StorageFilters 是 IconRecognition 合法 storageKind:categoryType 过滤器的命名空间。
type StorageFilters struct {
	Normal        NormalFilters
	ValuableDepot ValuableDepotFilters
	Isolate       IsolateFilters
}

// StorageFilter 创建不允许非法 storage/category 组合的字面量式过滤器访问。
func StorageFilter() StorageFilters {
	return StorageFilters{
		Normal: NormalFilters{
			Any:            ItemFilter("Normal:*"),
			Ore:            ItemFilter("Normal:Ore"),
			Plant:          ItemFilter("Normal:Plant"),
			Product:        ItemFilter("Normal:Product"),
			Doodad:         ItemFilter("Normal:Doodad"),
			Nurturance:     ItemFilter("Normal:Nurturance"),
			Usable:         ItemFilter("Normal:Usable"),
			Producer:       ItemFilter("Normal:Producer"),
			PortableDevice: ItemFilter("Normal:PortableDevice"),
		},
		ValuableDepot: ValuableDepotFilters{
			Any:            ItemFilter("ValuableDepot:*"),
			CommercialItem: ItemFilter("ValuableDepot:CommercialItem"),
			SpecialItem:    ItemFilter("ValuableDepot:SpecialItem"),
			Weapon:         ItemFilter("ValuableDepot:Weapon"),
		},
		Isolate: IsolateFilters{
			Any:           ItemFilter("Isolate:*"),
			AdventureExp:  ItemFilter("Isolate:AdventureExp"),
			BPExp:         ItemFilter("Isolate:BPExp"),
			Diamond:       ItemFilter("Isolate:Diamond"),
			DomainGold:    ItemFilter("Isolate:DomainGold"),
			Gold:          ItemFilter("Isolate:Gold"),
			Originium:     ItemFilter("Isolate:Originium"),
			SpaceshipGold: ItemFilter("Isolate:SpaceshipGold"),
			WeaponGold:    ItemFilter("Isolate:WeaponGold"),
		},
	}
}
