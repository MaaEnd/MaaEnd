package goods

import (
	"fmt"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/maafocus"
	sellstrategy "github.com/MaaXYZ/MaaEnd/agent/go-service/sellproduct/goods/strategy"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/sellproduct/internal/selectiondata"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

// LocationPlanItem 描述运行时据点计划中展示的一项货品。
type LocationPlanItem struct {
	Name            string
	ReserveQuantity int
}

// LocationPlan 表示据点运行时计划中的货品部分。
type LocationPlan struct {
	Strategy           sellstrategy.Kind
	Items              []LocationPlanItem
	ExcludedOutOfStock []string
	ReserveSatisfied   []LocationPlanItem
	ExcludedByUser     []string
}

// BuildLocationPlan 构建当前据点计划中的货品部分。
func BuildLocationPlan(location string) (LocationPlan, error) {
	groupsByLocation, err := loadItemPriorityGroupsFunc()
	if err != nil {
		return LocationPlan{}, fmt.Errorf("load item priorities: %w", err)
	}
	policy := priorityPolicySnapshot()
	groups := orderLocationPlanGroups(groupsByLocation[location], policy.Strategy, policy.StrategyConfig)
	groups = prioritizeItemGroups(groups, policy.Preferred, policy.OnlyPreferred)
	items, excludedOutOfStock, reserveSatisfied, excludedByUser := buildRuntimeLocationPlanItems(
		groups,
		reserveRulesSnapshot(),
		priorityOutOfStockSnapshot(),
		reserveSatisfiedItemsSnapshot(),
	)
	return LocationPlan{
		Strategy:           policy.Strategy,
		Items:              items,
		ExcludedOutOfStock: excludedOutOfStock,
		ReserveSatisfied:   reserveSatisfied,
		ExcludedByUser:     excludedByUser,
	}, nil
}

// orderLocationPlanGroups 使用当前静态选品策略生成进入据点时可展示的稳定售卖顺序。
// 库存策略需要先扫描画面，实际选择仍以扫描后的策略结果为准。
func orderLocationPlanGroups(
	groups []itemPriorityGroup,
	kind sellstrategy.Kind,
	config sellstrategy.Config,
) []itemPriorityGroup {
	groupsByID := make(map[string]itemPriorityGroup, len(groups))
	candidates := make([]sellstrategy.Candidate, 0, len(groups))
	for _, group := range groups {
		groupsByID[group.ItemID] = group
		candidates = append(candidates, sellstrategy.Candidate{
			ItemID:    group.ItemID,
			Rarity:    group.Rarity,
			UnitPrice: group.UnitPrice,
		})
	}
	selector, valid := sellstrategy.New(kind, config)
	orderer, static := selector.(sellstrategy.Orderer)
	if !valid || !static {
		orderer = sellstrategy.Rarity{}
	}
	ordered := orderer.Sort(candidates)
	result := make([]itemPriorityGroup, 0, len(ordered))
	for _, candidate := range ordered {
		result = append(result, groupsByID[candidate.ItemID])
	}
	return result
}

func buildRuntimeLocationPlanItems(
	groups []itemPriorityGroup,
	reserveRules map[string]int,
	outOfStock map[string]struct{},
	reserveSatisfied map[string]struct{},
) ([]LocationPlanItem, []string, []LocationPlanItem, []string) {
	items := make([]LocationPlanItem, 0, len(groups))
	excludedOutOfStock := make([]string, 0, len(outOfStock))
	satisfiedItems := make([]LocationPlanItem, 0, len(reserveSatisfied))
	excludedByUser := make([]string, 0)
	for _, group := range groups {
		if reserveRules[group.ItemID] == reserveBlacklistQuantity {
			excludedByUser = append(excludedByUser, group.DisplayName)
			continue
		}
		if _, unavailable := outOfStock[group.ItemID]; unavailable {
			excludedOutOfStock = append(excludedOutOfStock, group.DisplayName)
			continue
		}
		if _, satisfied := reserveSatisfied[group.ItemID]; satisfied {
			satisfiedItems = append(satisfiedItems, LocationPlanItem{
				Name:            group.DisplayName,
				ReserveQuantity: reserveRules[group.ItemID],
			})
			continue
		}
		items = append(items, LocationPlanItem{
			Name:            group.DisplayName,
			ReserveQuantity: reserveRules[group.ItemID],
		})
	}
	return items, excludedOutOfStock, satisfiedItems, excludedByUser
}

func printRuntimeOnlyPreferredEnabled(ctx *maa.Context) {
	maafocus.Print(ctx, runtimeOnlyPreferredEnabledMessage())
}

func printRuntimeStockPriorityEnabled(ctx *maa.Context, minimumPrice int) {
	maafocus.Print(ctx, runtimeStockPriorityEnabledMessage(minimumPrice))
}

func runtimeOnlyPreferredEnabledMessage() string {
	return i18n.T("sellproduct.runtime.only_preferred_enabled")
}

func runtimeStockPriorityEnabledMessage(minimumPrice int) string {
	return i18n.T("sellproduct.runtime.stock_priority_enabled", minimumPrice)
}

func printRuntimeItemSwitched(ctx *maa.Context, location string, itemID string) {
	maafocus.Print(ctx, runtimeItemSwitchedMessage(location, itemID))
}

func runtimeItemSwitchedMessage(location string, itemID string) string {
	return i18n.T(
		"sellproduct.runtime.item_switched",
		runtimeItemName(itemID),
		runtimeLocationName(location),
	)
}

func printRuntimeItemOutOfStock(ctx *maa.Context, location string, itemID string) {
	maafocus.Print(ctx, runtimeItemOutOfStockMessage(location, itemID))
}

func runtimeItemOutOfStockMessage(location string, itemID string) string {
	return i18n.T(
		"sellproduct.runtime.item_out_of_stock",
		runtimeItemName(itemID),
		runtimeLocationName(location),
	)
}

func printRuntimeReserveSatisfied(ctx *maa.Context, itemID string, quantity int) {
	maafocus.Print(ctx, runtimeReserveSatisfiedMessage(itemID, quantity))
}

func runtimeReserveSatisfiedMessage(itemID string, quantity int) string {
	return i18n.T(
		"sellproduct.runtime.reserve_satisfied",
		runtimeItemName(itemID),
		quantity,
	)
}

func runtimeLocationName(location string) string {
	data, err := selectiondata.LoadCached()
	if err == nil {
		if entry, ok := data.Locations[location]; ok {
			return selectiondata.LocalizedName(entry.Names, location)
		}
	}
	return location
}

func runtimeItemName(itemID string) string {
	data, err := selectiondata.LoadCached()
	if err == nil {
		if entry, ok := data.Items[itemID]; ok {
			return selectiondata.LocalizedName(entry.Names, itemID)
		}
	}
	return itemID
}
