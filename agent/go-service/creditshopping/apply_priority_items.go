package creditshopping

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const (
	applyPriorityItemsActionName = "CreditShoppingApplyPriorityItemsAction"
	applyPriorityItemsNodeName   = "CreditShoppingApplyPriorityItems"
)

// ApplyPriorityItemsAction 把任务选项的勾选（节点 attach）复制成各档 IconRecognition 的 item_ids。
type ApplyPriorityItemsAction struct{}

var _ maa.CustomActionRunner = (*ApplyPriorityItemsAction)(nil)

func (a *ApplyPriorityItemsAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if ctx == nil {
		log.Error().Str("component", component).Msg("apply priority items: nil context")
		return false
	}
	if arg == nil {
		log.Error().Str("component", component).Msg("apply priority items: nil custom action arg")
		return false
	}

	levels := attachPriorityItems(ctx, applyPriorityItemsNodeName)
	if len(levels) == 0 {
		log.Debug().Str("component", component).Msg("apply priority items: empty selection, skip OverridePipeline")
		return true
	}

	patch := make(map[string]any, len(levels))
	for level, caseIDs := range levels {
		itemIDs, err := recognitionItemIDs(caseIDs)
		if err != nil {
			log.Error().Err(err).Int("priority", level).Str("component", component).Msg("apply priority items: build override failed")
			return false
		}
		patch[priorityItemNodeName(level)] = priorityItemNode(itemIDs)
	}

	if err := ctx.OverridePipeline(patch); err != nil {
		log.Error().Err(err).Str("component", component).Msg("apply priority items: OverridePipeline failed")
		return false
	}

	log.Info().
		Str("component", component).
		Interface("priority1", levels[1]).
		Interface("priority2", levels[2]).
		Interface("priority3", levels[3]).
		Msg("apply priority items: OverridePipeline applied")
	return true
}

// priorityItemNode 是三档物品节点的识别参数；ROI 由 resource / resource_adb 覆盖，这里不写。
func priorityItemNode(itemIDs []string) map[string]any {
	return map[string]any{
		"recognition":        "Custom",
		"custom_recognition": "IconRecognition",
		"index":              0,
		"custom_recognition_param": map[string]any{
			"grid_type":   "credit_trade",
			"item_ids":    itemIDs,
			"order_by":    "natural",
			"deduplicate": true,
		},
	}
}

func priorityItemNodeName(level int) string {
	return "CreditShoppingPriority" + strconv.Itoa(level) + "Item"
}

// attachPriorityItems 读取节点 attach 中 priority{N}__<CaseID> 的勾选项，按档位分组。
func attachPriorityItems(ctx *maa.Context, nodeName string) map[int][]string {
	raw, err := ctx.GetNodeJSON(nodeName)
	if err != nil || raw == "" {
		return nil
	}
	var node struct {
		Attach map[string]json.RawMessage `json:"attach"`
	}
	if err := json.Unmarshal([]byte(raw), &node); err != nil || len(node.Attach) == 0 {
		return nil
	}

	levels := map[int][]string{}
	for key, rawValue := range node.Attach {
		level, caseID, ok := parseAttachKey(key)
		if !ok || !attachEnabled(rawValue) {
			continue
		}
		levels[level] = append(levels[level], caseID)
	}
	return levels
}

// parseAttachKey 从 attach 键（priority{N}__<CaseID>）里取档位与 case 名。
func parseAttachKey(key string) (level int, caseID string, ok bool) {
	for level := 1; level <= 3; level++ {
		if rest, found := strings.CutPrefix(key, "priority"+strconv.Itoa(level)+"__"); found && rest != "" {
			return level, rest, true
		}
	}
	return 0, "", false
}

// attachEnabled 判断 attach 值是否表示勾选：null、空串与 "false" 视为未勾选。
func attachEnabled(raw json.RawMessage) bool {
	if len(raw) == 0 || string(raw) == "null" {
		return false
	}
	var asBool bool
	if err := json.Unmarshal(raw, &asBool); err == nil {
		return asBool
	}
	var asString string
	if err := json.Unmarshal(raw, &asString); err == nil {
		return strings.TrimSpace(asString) != "" && !strings.EqualFold(asString, "false")
	}
	return true
}

// recognitionItemIDs 把 case 名换成 IconRecognition 的 catalog item_id，重复项只保留一次。
func recognitionItemIDs(caseIDs []string) ([]string, error) {
	itemIDs := make([]string, 0, len(caseIDs))
	seen := make(map[string]struct{}, len(caseIDs))
	for _, caseID := range caseIDs {
		itemID, ok := creditItemIDByCase[caseID]
		if !ok {
			return nil, fmt.Errorf("unknown item id %q", caseID)
		}
		if _, dup := seen[itemID]; dup {
			continue
		}
		seen[itemID] = struct{}{}
		itemIDs = append(itemIDs, itemID)
	}
	return itemIDs, nil
}
