package sellproduct

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const reserveSessionActionName = "SellProductReserveSession"

const (
	reserveOperationReset    = "reset"
	reserveOperationRegister = "register"
	reserveOperationSelect   = "select"
	reserveOperationApply    = "apply"
)

var (
	reserveSessionMu sync.Mutex
	reserveRules     = map[string]int{}
	reserveSelected  string
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
		replaced := registerReserveRule(param.ItemID, param.Quantity)
		event := log.Info()
		if replaced {
			event = log.Warn()
		}
		event.Str("component", reserveSessionActionName).
			Str("item_id", param.ItemID).
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
		if err := ctx.OverridePipeline(buildReserveSlidingOverride(param.SlidingNode, quantity, configured)); err != nil {
			log.Error().Err(err).
				Str("component", reserveSessionActionName).
				Str("sliding_node", param.SlidingNode).
				Msg("failed to apply reserve rule")
			return false
		}
		log.Info().Str("component", reserveSessionActionName).
			Str("item_id", itemID).
			Int("quantity", quantity).
			Bool("configured", configured).
			Str("sliding_node", param.SlidingNode).
			Msg("reserve rule applied")
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
	case reserveOperationReset:
	case reserveOperationRegister, reserveOperationSelect:
		if param.ItemID == "" {
			return nil, fmt.Errorf("item_id is empty")
		}
		if param.Operation == reserveOperationRegister && param.Quantity < 0 {
			return nil, fmt.Errorf("quantity must not be negative")
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

func resetReserveSession() {
	reserveSessionMu.Lock()
	defer reserveSessionMu.Unlock()
	reserveRules = map[string]int{}
	reserveSelected = ""
}

// registerReserveRule 返回该物品是否已存在规则。后注册的槽位覆盖先注册的槽位。
func registerReserveRule(itemID string, quantity int) bool {
	reserveSessionMu.Lock()
	defer reserveSessionMu.Unlock()
	_, replaced := reserveRules[itemID]
	reserveRules[itemID] = quantity
	return replaced
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

func buildReserveSlidingOverride(slidingNode string, quantity int, configured bool) map[string]any {
	if configured {
		return map[string]any{
			slidingNode: map[string]any{
				"next": []string{
					"SellProductSkipToNextSellLoop",
					"SellProductSellThenLoop",
				},
				"attach": map[string]any{
					"Target":        quantity,
					"TargetReverse": true,
				},
			},
		}
	}
	return map[string]any{
		slidingNode: map[string]any{
			"next": []string{"SellProductSell"},
			"attach": map[string]any{
				"Target":        999999,
				"TargetReverse": false,
			},
		},
	}
}
