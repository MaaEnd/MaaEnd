package goods

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const reserveSessionActionName = "OutpostTradingReserveSession"

// reserveBlacklistQuantity 是任务配置中“永不售卖”的唯一哨兵值。
// 仅接受 -1，避免把其他负数静默解释为有效规则。
const reserveBlacklistQuantity = -1

const (
	reserveOperationReset    = "reset"
	reserveOperationRegister = "register"
	reserveOperationSelect   = "select"
	reserveOperationApply    = "apply"
	reserveOperationSatisfy  = "satisfy"
	reserveOperationSkip     = "skip"
)

var (
	reserveSessionMu      sync.Mutex
	reserveRules          = map[string]int{}
	reserveSatisfiedItems = map[string]struct{}{}
	reserveSelected       string
)

type reserveSessionActionParam struct {
	Operation   string `json:"operation"`
	ItemID      string `json:"item_id,omitempty"`
	Quantity    int    `json:"quantity,omitempty"`
	SlidingNode string `json:"sliding_node,omitempty"`
}

// ReserveSessionAction 只维护任务级保留规则，并在执行数量滑块前覆盖对应节点参数。
// 界面跳转和售卖循环仍由 Pipeline 串联，Go 不接管业务流程。
type ReserveSessionAction struct{}

var _ maa.CustomActionRunner = (*ReserveSessionAction)(nil)

func (a *ReserveSessionAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if arg == nil {
		log.Error().Str("component", reserveSessionActionName).Msg("custom action arg is nil")
		return false
	}
	param, err := parseReserveSessionActionParam(arg.CustomActionParam)
	if err != nil {
		log.Error().Err(err).Str("component", reserveSessionActionName).Msg("invalid params")
		return false
	}

	switch param.Operation {
	case reserveOperationReset:
		resetReserveSession()
		return true
	case reserveOperationRegister:
		itemID := param.ItemID
		if itemID == "" {
			var err error
			itemID, err = loadReserveItemID(ctx, arg.CurrentTaskName)
			if err != nil {
				log.Error().Err(err).
					Str("component", reserveSessionActionName).
					Str("node", arg.CurrentTaskName).
					Msg("failed to load reserve item")
				return false
			}
		}
		if itemID == "" {
			log.Debug().Str("component", reserveSessionActionName).
				Str("node", arg.CurrentTaskName).
				Msg("unconfigured reserve rule slot skipped")
			return true
		}
		replaced := registerReserveRule(itemID, param.Quantity)
		event := log.Info()
		if replaced {
			event = log.Warn()
		}
		event.Str("component", reserveSessionActionName).
			Str("item_id", itemID).
			Int("quantity", param.Quantity).
			Bool("replaced", replaced).
			Msg("reserve rule registered")
		return true
	case reserveOperationSelect:
		setSelectedReserveItem(param.ItemID)
		log.Debug().Str("component", reserveSessionActionName).
			Str("item_id", param.ItemID).
			Msg("selected item recorded")
		return true
	case reserveOperationApply:
		if ctx == nil {
			log.Error().Str("component", reserveSessionActionName).Msg("context is nil")
			return false
		}
		itemID, quantity, configured := selectedReserveRule()
		// 黑名单应在选货识别阶段被排除；若仍到达滑块，说明运行期状态违背契约。
		// 此处宁可停止当前流程，也不能回退到默认“全部售卖”。
		if quantity == reserveBlacklistQuantity {
			log.Error().Str("component", reserveSessionActionName).
				Str("item_id", itemID).
				Msg("blacklisted item reached reserve rule application")
			return false
		}
		aidQuota := resolveAidQuotaLimit(ctx, itemID, quantity)
		if err := ctx.OverridePipeline(buildReserveSlidingOverride(param.SlidingNode, quantity, configured, aidQuota)); err != nil {
			log.Error().Err(err).
				Str("component", reserveSessionActionName).
				Str("sliding_node", param.SlidingNode).
				Msg("failed to apply reserve rule")
			return false
		}
		// 调度券不足以兑换一件活动物品时，当前物品本次任务跳过，直接回到售卖循环换下一个。
		if aidQuota.Applied && aidQuota.Target < 1 {
			if err := ctx.OverrideNext(param.SlidingNode, []maa.NextItem{{Name: "OutpostTradingAidQuotaExhausted"}}); err != nil {
				log.Error().Err(err).
					Str("component", reserveSessionActionName).
					Str("sliding_node", param.SlidingNode).
					Msg("failed to override next for aid quota exhausted")
				return false
			}
		}
		log.Info().Str("component", reserveSessionActionName).
			Str("item_id", itemID).
			Int("quantity", quantity).
			Bool("configured", configured).
			Bool("aid_quota_applied", aidQuota.Applied).
			Int("aid_quota_balance", aidQuota.Balance).
			Int("aid_quota_limit", aidQuota.Limit).
			Str("sliding_node", param.SlidingNode).
			Msg("reserve rule applied")
		if aidQuota.Applied && aidQuota.Target > 0 {
			printRuntimeAidQuotaLimited(ctx, aidQuota)
		}
		return true
	case reserveOperationSatisfy:
		itemID, quantity, marked, ok := markSelectedReserveSatisfied()
		if !ok {
			log.Error().Str("component", reserveSessionActionName).
				Str("item_id", itemID).
				Int("quantity", quantity).
				Msg("cannot satisfy unconfigured reserve rule")
			return false
		}
		log.Info().Str("component", reserveSessionActionName).
			Str("item_id", itemID).
			Int("quantity", quantity).
			Bool("marked", marked).
			Msg("reserve quantity satisfied for current task")
		if marked {
			printRuntimeReserveSatisfied(ctx, itemID, quantity)
		}
		return true
	case reserveOperationSkip:
		// 调度券不足以兑换一件当前物品时，无条件把该物品标记为本次任务跳过。
		// 与 satisfy 不同，它不要求该物品配置过保留规则。
		itemID, marked := markSelectedReserveSkipped()
		if itemID == "" {
			log.Error().Str("component", reserveSessionActionName).
				Msg("skip has no selected item")
			return false
		}
		log.Info().Str("component", reserveSessionActionName).
			Str("item_id", itemID).
			Bool("marked", marked).
			Msg("item skipped for current task due to insufficient aid quota")
		return true
	default:
		return false
	}
}

func parseReserveSessionActionParam(raw string) (*reserveSessionActionParam, error) {
	var param reserveSessionActionParam
	if err := json.Unmarshal([]byte(raw), &param); err != nil {
		return nil, fmt.Errorf("unmarshal custom_action_param: %w", err)
	}
	param.Operation = strings.TrimSpace(param.Operation)
	param.ItemID = strings.TrimSpace(param.ItemID)
	param.SlidingNode = strings.TrimSpace(param.SlidingNode)
	switch param.Operation {
	case reserveOperationReset, reserveOperationSatisfy, reserveOperationSkip:
	case reserveOperationRegister:
		if param.Quantity < reserveBlacklistQuantity {
			return nil, fmt.Errorf("quantity must be -1 or greater")
		}
	case reserveOperationSelect:
		if param.ItemID == "" {
			return nil, fmt.Errorf("item_id is empty")
		}
	case reserveOperationApply:
		if param.SlidingNode == "" {
			return nil, fmt.Errorf("sliding_node is empty")
		}
	default:
		return nil, fmt.Errorf("invalid operation %q", param.Operation)
	}
	return &param, nil
}

func loadReserveItemID(ctx *maa.Context, nodeName string) (string, error) {
	if ctx == nil {
		return "", fmt.Errorf("context is nil")
	}
	nodeName = strings.TrimSpace(nodeName)
	if nodeName == "" {
		return "", fmt.Errorf("node name is empty")
	}
	raw, err := ctx.GetNodeJSON(nodeName)
	if err != nil {
		return "", fmt.Errorf("get node %s json: %w", nodeName, err)
	}
	return parseReserveItemIDAttach(raw, nodeName)
}

func parseReserveItemIDAttach(raw string, nodeName string) (string, error) {
	var wrapper struct {
		Attach struct {
			ItemID string `json:"item_id"`
		} `json:"attach"`
	}
	if err := json.Unmarshal([]byte(raw), &wrapper); err != nil {
		return "", fmt.Errorf("unmarshal %s attach: %w", nodeName, err)
	}
	itemID := strings.TrimSpace(wrapper.Attach.ItemID)
	return itemID, nil
}

func resetReserveSession() {
	reserveSessionMu.Lock()
	reserveRules = map[string]int{}
	reserveSatisfiedItems = map[string]struct{}{}
	reserveSelected = ""
	reserveSessionMu.Unlock()
	resetPrioritySelectionSession()
}

// markSelectedReserveSkipped 无条件把当前选中物品标记为本次任务跳过（加入已达保留量集合，
// 选品阶段会直接排除）。返回 marked=false 表示该物品此前已标记。与 satisfy 的区别是
// 它不要求该物品配置过保留规则，用于调度券不足以兑换一件时的主动跳过。
func markSelectedReserveSkipped() (itemID string, marked bool) {
	reserveSessionMu.Lock()
	defer reserveSessionMu.Unlock()
	itemID = reserveSelected
	if itemID == "" {
		return "", false
	}
	_, exists := reserveSatisfiedItems[itemID]
	reserveSatisfiedItems[itemID] = struct{}{}
	return itemID, !exists
}

// markSelectedReserveSatisfied 在 BetterSliding 确认无需交易，或可达目标的交易成功确认后，
// 将当前物品标记为本次任务无需再次处理。返回 marked=false 表示该物品此前已标记。
func markSelectedReserveSatisfied() (itemID string, quantity int, marked bool, ok bool) {
	reserveSessionMu.Lock()
	defer reserveSessionMu.Unlock()
	itemID = reserveSelected
	quantity, exists := reserveRules[itemID]
	if itemID == "" || !exists || quantity <= 0 {
		return itemID, quantity, false, false
	}
	_, exists = reserveSatisfiedItems[itemID]
	reserveSatisfiedItems[itemID] = struct{}{}
	return itemID, quantity, !exists, true
}

// reserveSatisfiedItemsSnapshot 返回本次任务已经达到保留数量的物品集合。
// 该状态与用户黑名单、运行期缺货分别维护，避免混淆排除原因。
func reserveSatisfiedItemsSnapshot() map[string]struct{} {
	reserveSessionMu.Lock()
	defer reserveSessionMu.Unlock()
	satisfied := make(map[string]struct{}, len(reserveSatisfiedItems))
	for itemID := range reserveSatisfiedItems {
		satisfied[itemID] = struct{}{}
	}
	return satisfied
}

// registerReserveRule 返回该物品是否已存在规则。后注册的槽位覆盖先注册的槽位。
func registerReserveRule(itemID string, quantity int) bool {
	reserveSessionMu.Lock()
	defer reserveSessionMu.Unlock()
	_, replaced := reserveRules[itemID]
	reserveRules[itemID] = quantity
	return replaced
}

func reserveRulesSnapshot() map[string]int {
	reserveSessionMu.Lock()
	defer reserveSessionMu.Unlock()
	snapshot := make(map[string]int, len(reserveRules))
	for itemID, quantity := range reserveRules {
		snapshot[itemID] = quantity
	}
	return snapshot
}

// reserveBlacklistedItemsSnapshot 返回本次任务明确配置为永不售卖的物品集合。
// 黑名单与运行期缺货分别维护，避免用户配置被错误记录为缺货。
func reserveBlacklistedItemsSnapshot() map[string]struct{} {
	reserveSessionMu.Lock()
	defer reserveSessionMu.Unlock()
	blacklisted := make(map[string]struct{})
	for itemID, quantity := range reserveRules {
		if quantity == reserveBlacklistQuantity {
			blacklisted[itemID] = struct{}{}
		}
	}
	return blacklisted
}

func setSelectedReserveItem(itemID string) {
	reserveSessionMu.Lock()
	defer reserveSessionMu.Unlock()
	reserveSelected = strings.TrimSpace(itemID)
}

func selectedReserveRule() (itemID string, quantity int, configured bool) {
	reserveSessionMu.Lock()
	defer reserveSessionMu.Unlock()
	itemID = reserveSelected
	quantity, configured = reserveRules[itemID]
	// 保留 0 等价于不启用保留，继续使用默认“全部售出”路径。
	configured = configured && quantity > 0
	return itemID, quantity, configured
}

// buildReserveSlidingOverride 组装覆盖 BetterSliding 滑动节点的参数。
//
// configured 为 true 时按保留规则只卖超出保留量的部分；否则默认全部售出。
// aidQuota.Applied 为 true 时（活动物品），目标数量改为调度券余量可兑换的上限：
// 有保留规则时还需读取库存，卖出「超出保留量且不超调度券上限」的部分；
// 调度券不足以兑换一件时目标为 0，BetterSliding 判定 OutOfRange 后启用
// OutpostTradingAidQuotaExhausted 跳过该物品。
func buildReserveSlidingOverride(slidingNode string, quantity int, configured bool, aidQuota aidQuotaDecision) map[string]any {
	if !aidQuota.Applied {
		if configured {
			return map[string]any{
				slidingNode: map[string]any{
					"next": []string{
						"OutpostTradingReserveAlreadySatisfied",
						"OutpostTradingSellThenLoop",
					},
					"attach": map[string]any{
						"TargetQuantity": quantity,
						"ReverseTarget":  true,
					},
				},
			}
		}
		return map[string]any{
			slidingNode: map[string]any{
				"next": []string{"OutpostTradingSell"},
				"attach": map[string]any{
					"TargetQuantity": 999999,
					"ReverseTarget":  false,
				},
			},
		}
	}

	// 活动物品：目标已在 Go 侧算好，直接关闭 ReverseTarget。
	// Target < 1 的情况已由调用方改写 sliding 节点的 next 跳到 AidQuotaExhausted，
	// 不会进入 BetterSliding；其余情况 ClampTargetToSliderMax 保证目标不超过库存。
	return map[string]any{
		slidingNode: map[string]any{
			"next": []string{
				"OutpostTradingAidQuotaExhausted",
				"OutpostTradingSellThenLoop",
			},
			"attach": map[string]any{
				"TargetQuantity": aidQuota.Target,
				"ReverseTarget":  false,
			},
		},
	}
}
