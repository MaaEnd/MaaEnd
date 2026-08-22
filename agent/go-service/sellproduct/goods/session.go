package goods

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"

	sellstrategy "github.com/MaaXYZ/MaaEnd/agent/go-service/sellproduct/goods/strategy"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const (
	priorityItemRecognitionName = "SellProductPriorityItem"
	prioritySessionActionName   = "SellProductPrioritySession"

	priorityResultSelect            = "select"
	priorityResultExhausted         = "exhausted"
	priorityOperationConfigure      = "configure"
	priorityOperationStrategy       = "configure_strategy"
	priorityOperationResetItems     = "reset_preferred"
	priorityOperationResetSelection = "reset_goods_selection"
	priorityOperationRegister       = "register"
	priorityOperationCommit         = "commit"
	priorityOperationAdopt          = "adopt"
	priorityOperationOutOfStock     = "out_of_stock"
)

type prioritySessionActionParam struct {
	Operation     string            `json:"operation"`
	Enabled       bool              `json:"enabled,omitempty"`
	OnlyPreferred bool              `json:"only_preferred,omitempty"`
	Strategy      sellstrategy.Kind `json:"strategy,omitempty"`
	MinimumPrice  int               `json:"minimum_unit_price,omitempty"`
	Location      string            `json:"location,omitempty"`
	ItemID        string            `json:"item_id,omitempty"`
}

type prioritySelectionPolicy struct {
	Preferred      []string
	OnlyPreferred  bool
	Strategy       sellstrategy.Kind
	StrategyConfig sellstrategy.Config
}

type priorityExhaustionObservation struct {
	Signature string
	Count     int
}

type goodsSelectionSessionState struct {
	Location         string
	ExhaustedItemIDs []string
}

type prioritySelectionSessionState struct {
	Enabled        bool
	OnlyPreferred  bool
	RegionEnabled  bool
	Strategy       sellstrategy.Kind
	StrategyConfig sellstrategy.Config
	Preferred      []string
	Attempted      map[string]map[string]struct{}
	Pending        map[string]string
	Current        map[string]string
	OutOfStock     map[string]struct{}
	Exhaustion     map[string]priorityExhaustionObservation
	GoodsSelection goodsSelectionSessionState
}

var (
	prioritySelectionMu sync.Mutex
	prioritySelection   = newPrioritySelectionSessionState()
)

// PrioritySessionAction 在初始化阶段配置总开关，进入地区时切换该地区的用户优先级，
// 在 Pipeline 确认换货后提交待选结果，并维护任务级共享缺货集合。
type PrioritySessionAction struct{}

var _ maa.CustomActionRunner = (*PrioritySessionAction)(nil)

func (a *PrioritySessionAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	param, err := parsePrioritySessionActionParam(arg)
	if err != nil {
		log.Error().Err(err).Str("component", prioritySessionActionName).Msg("invalid params")
		return false
	}
	switch param.Operation {
	case priorityOperationConfigure:
		configurePrioritySession(param.Enabled, param.OnlyPreferred)
		log.Info().Str("component", prioritySessionActionName).
			Bool("enabled", param.Enabled).
			Bool("only_preferred", param.OnlyPreferred).
			Msg("priority selling configured")
		if param.Enabled && param.OnlyPreferred {
			printRuntimeOnlyPreferredEnabled(ctx)
		}
		return true
	case priorityOperationStrategy:
		configureSelectionStrategy(param.Strategy, sellstrategy.Config{MinimumUnitPrice: param.MinimumPrice})
		log.Info().Str("component", prioritySessionActionName).
			Str("strategy", string(param.Strategy)).
			Int("minimum_unit_price", param.MinimumPrice).
			Msg("goods selection strategy configured")
		if param.Strategy == sellstrategy.KindStock {
			printRuntimeStockPriorityEnabled(ctx, param.MinimumPrice)
		}
		return true
	case priorityOperationResetItems:
		resetPreferredPriorityItems(param.Enabled)
		return true
	case priorityOperationResetSelection:
		resetGoodsSelection()
		return true
	case priorityOperationRegister:
		if param.ItemID == "" {
			log.Debug().Str("component", prioritySessionActionName).
				Msg("unconfigured priority item slot skipped")
			return true
		}
		registered := registerPriorityItem(param.ItemID)
		log.Info().Str("component", prioritySessionActionName).
			Str("item_id", param.ItemID).
			Bool("registered", registered).
			Msg("preferred selling item registered")
		return true
	case priorityOperationCommit:
		itemID, ok := prioritySelectionCommit(param.Location)
		if !ok {
			log.Error().Str("component", prioritySessionActionName).Str("location", param.Location).
				Msg("priority selection commit has no pending item")
			return false
		}
		setSelectedReserveItem(itemID)
		printRuntimeItemSwitched(ctx, param.Location, itemID)
		return true
	case priorityOperationAdopt:
		// 沿用界面识别到的当前货品，效果等同换货提交：记录据点当前物品、
		// 更新保留规则选中项，后续售卖与缺货标记流程无需区分来源。
		itemID, err := currentGoodsItemIDFromRecognition(arg.RecognitionDetail)
		if err != nil {
			log.Error().Err(err).
				Str("component", prioritySessionActionName).
				Str("location", param.Location).
				Msg("adopt has no recognized current goods")
			return false
		}
		prioritySelectionAdopt(param.Location, itemID)
		setSelectedReserveItem(itemID)
		log.Info().Str("component", prioritySessionActionName).
			Str("location", param.Location).
			Str("item_id", itemID).
			Msg("current goods adopted")
		printRuntimeItemAdopted(ctx, param.Location, itemID)
		return true
	case priorityOperationOutOfStock:
		itemID, marked, ok := prioritySelectionMarkOutOfStock(param.Location)
		if !ok {
			log.Error().Str("component", prioritySessionActionName).Str("location", param.Location).
				Msg("out-of-stock mark has no committed item")
			return false
		}
		log.Info().Str("component", prioritySessionActionName).
			Str("location", param.Location).
			Str("item_id", itemID).
			Bool("marked", marked).
			Msg("selling item marked out of stock for current task")
		if marked {
			printRuntimeItemOutOfStock(ctx, param.Location, itemID)
		}
		return true
	default:
		return false
	}
}

func parsePrioritySessionActionParam(arg *maa.CustomActionArg) (*prioritySessionActionParam, error) {
	if arg == nil {
		return nil, fmt.Errorf("custom action arg is nil")
	}
	var param prioritySessionActionParam
	if err := json.Unmarshal([]byte(arg.CustomActionParam), &param); err != nil {
		return nil, fmt.Errorf("unmarshal custom_action_param: %w", err)
	}
	param.Operation = strings.TrimSpace(param.Operation)
	param.Location = strings.TrimSpace(param.Location)
	param.ItemID = strings.TrimSpace(param.ItemID)
	switch param.Operation {
	case priorityOperationConfigure, priorityOperationResetItems, priorityOperationResetSelection:
	case priorityOperationStrategy:
		if _, ok := sellstrategy.New(param.Strategy, sellstrategy.Config{MinimumUnitPrice: param.MinimumPrice}); !ok {
			return nil, fmt.Errorf("invalid selection strategy %q", param.Strategy)
		}
	case priorityOperationRegister:
	case priorityOperationCommit, priorityOperationAdopt, priorityOperationOutOfStock:
		if param.Location == "" {
			return nil, fmt.Errorf("location is empty")
		}
	default:
		return nil, fmt.Errorf("invalid operation %q", param.Operation)
	}
	return &param, nil
}

func newPrioritySelectionSessionState() prioritySelectionSessionState {
	return prioritySelectionSessionState{
		Enabled:        false,
		OnlyPreferred:  false,
		RegionEnabled:  false,
		Strategy:       sellstrategy.KindRarity,
		Preferred:      []string{},
		Attempted:      map[string]map[string]struct{}{},
		Pending:        map[string]string{},
		Current:        map[string]string{},
		OutOfStock:     map[string]struct{}{},
		Exhaustion:     map[string]priorityExhaustionObservation{},
		GoodsSelection: goodsSelectionSessionState{},
	}
}

func resetGoodsSelection() {
	prioritySelectionMu.Lock()
	defer prioritySelectionMu.Unlock()
	prioritySelection.GoodsSelection = goodsSelectionSessionState{}
}

func beginGoodsSelection(location string) {
	prioritySelectionMu.Lock()
	defer prioritySelectionMu.Unlock()
	prioritySelection.GoodsSelection = goodsSelectionSessionState{Location: location}
}

func setGoodsSelectionExhaustedItems(location string, itemIDs []string) {
	prioritySelectionMu.Lock()
	defer prioritySelectionMu.Unlock()
	if prioritySelection.GoodsSelection.Location == location {
		prioritySelection.GoodsSelection.ExhaustedItemIDs = append([]string{}, itemIDs...)
	}
}

func goodsSelectionExhaustedItemsSnapshot(location string) []string {
	prioritySelectionMu.Lock()
	defer prioritySelectionMu.Unlock()
	if prioritySelection.GoodsSelection.Location != location {
		return nil
	}
	return append([]string{}, prioritySelection.GoodsSelection.ExhaustedItemIDs...)
}

// registerPriorityItem 返回是否成功登记。重复物品保留首次出现的槽位顺序。
func registerPriorityItem(itemID string) bool {
	prioritySelectionMu.Lock()
	defer prioritySelectionMu.Unlock()
	for _, registered := range prioritySelection.Preferred {
		if registered == itemID {
			return false
		}
	}
	prioritySelection.Preferred = append(prioritySelection.Preferred, itemID)
	return true
}

func priorityPolicySnapshot() prioritySelectionPolicy {
	prioritySelectionMu.Lock()
	defer prioritySelectionMu.Unlock()
	policy := prioritySelectionPolicy{
		Strategy:       prioritySelection.Strategy,
		StrategyConfig: prioritySelection.StrategyConfig,
	}
	if !prioritySelection.Enabled || !prioritySelection.RegionEnabled {
		return policy
	}
	policy.Preferred = append([]string{}, prioritySelection.Preferred...)
	policy.OnlyPreferred = prioritySelection.OnlyPreferred
	return policy
}

func priorityOutOfStockSnapshot() map[string]struct{} {
	prioritySelectionMu.Lock()
	defer prioritySelectionMu.Unlock()
	outOfStock := make(map[string]struct{}, len(prioritySelection.OutOfStock))
	for itemID := range prioritySelection.OutOfStock {
		outOfStock[itemID] = struct{}{}
	}
	return outOfStock
}

func prioritySelectionMarkScannedOutOfStock(items []stockPageItem) {
	prioritySelectionMu.Lock()
	defer prioritySelectionMu.Unlock()
	for _, item := range items {
		if !item.StockKnown || item.Quantity > 0 {
			continue
		}
		prioritySelection.OutOfStock[item.ItemID] = struct{}{}
	}
}

func resetPrioritySelectionSession() {
	prioritySelectionMu.Lock()
	defer prioritySelectionMu.Unlock()
	prioritySelection = newPrioritySelectionSessionState()
}

func configurePrioritySession(enabled, onlyPreferred bool) {
	prioritySelectionMu.Lock()
	defer prioritySelectionMu.Unlock()
	prioritySelection.Enabled = enabled
	prioritySelection.OnlyPreferred = enabled && onlyPreferred
}

func configureSelectionStrategy(kind sellstrategy.Kind, config sellstrategy.Config) {
	prioritySelectionMu.Lock()
	defer prioritySelectionMu.Unlock()
	prioritySelection.Strategy = kind
	prioritySelection.StrategyConfig = config
}

// resetPreferredPriorityItems 切换当前地区是否启用优先配置并清空地区优先表，
// 同时保留任务内已尝试物品、待提交状态和共享缺货集合。
func resetPreferredPriorityItems(enabled bool) {
	prioritySelectionMu.Lock()
	defer prioritySelectionMu.Unlock()
	prioritySelection.RegionEnabled = enabled
	prioritySelection.Preferred = []string{}
}

func prioritySelectionSnapshot(location string) (map[string]struct{}, map[string]struct{}, string) {
	prioritySelectionMu.Lock()
	defer prioritySelectionMu.Unlock()
	attempted := make(map[string]struct{}, len(prioritySelection.Attempted[location]))
	for itemID := range prioritySelection.Attempted[location] {
		attempted[itemID] = struct{}{}
	}
	outOfStock := make(map[string]struct{}, len(prioritySelection.OutOfStock))
	for itemID := range prioritySelection.OutOfStock {
		outOfStock[itemID] = struct{}{}
	}
	return attempted, outOfStock, prioritySelection.Pending[location]
}

func prioritySelectionSetPending(location, itemID string) {
	prioritySelectionMu.Lock()
	defer prioritySelectionMu.Unlock()
	prioritySelection.Pending[location] = itemID
	delete(prioritySelection.Exhaustion, location)
}

func prioritySelectionCommit(location string) (string, bool) {
	prioritySelectionMu.Lock()
	defer prioritySelectionMu.Unlock()
	itemID := prioritySelection.Pending[location]
	if itemID == "" {
		return "", false
	}
	if prioritySelection.Attempted[location] == nil {
		prioritySelection.Attempted[location] = map[string]struct{}{}
	}
	prioritySelection.Attempted[location][itemID] = struct{}{}
	prioritySelection.Current[location] = itemID
	delete(prioritySelection.Pending, location)
	delete(prioritySelection.Exhaustion, location)
	return itemID, true
}

// prioritySelectionAdopt 把界面识别到的当前货品登记为据点当前售卖物品，
// 状态效果与换货提交一致：记录已尝试、清空待选和耗尽观察。
func prioritySelectionAdopt(location, itemID string) {
	prioritySelectionMu.Lock()
	defer prioritySelectionMu.Unlock()
	if prioritySelection.Attempted[location] == nil {
		prioritySelection.Attempted[location] = map[string]struct{}{}
	}
	prioritySelection.Attempted[location][itemID] = struct{}{}
	prioritySelection.Current[location] = itemID
	delete(prioritySelection.Pending, location)
	delete(prioritySelection.Exhaustion, location)
}

func prioritySelectionMarkOutOfStock(location string) (string, bool, bool) {
	prioritySelectionMu.Lock()
	defer prioritySelectionMu.Unlock()
	itemID := prioritySelection.Current[location]
	if itemID == "" {
		return "", false, false
	}
	_, exists := prioritySelection.OutOfStock[itemID]
	prioritySelection.OutOfStock[itemID] = struct{}{}
	delete(prioritySelection.Exhaustion, location)
	return itemID, !exists, true
}

func prioritySelectionResetExhaustion(location string) {
	prioritySelectionMu.Lock()
	defer prioritySelectionMu.Unlock()
	delete(prioritySelection.Exhaustion, location)
}

func prioritySelectionObserveExhaustion(location string, recognized []string) bool {
	ids := append([]string{}, recognized...)
	sort.Strings(ids)
	signature := strings.Join(ids, "|")
	prioritySelectionMu.Lock()
	defer prioritySelectionMu.Unlock()
	observation := prioritySelection.Exhaustion[location]
	if observation.Signature == signature {
		observation.Count++
	} else {
		observation = priorityExhaustionObservation{Signature: signature, Count: 1}
	}
	prioritySelection.Exhaustion[location] = observation
	return observation.Count >= 2
}
