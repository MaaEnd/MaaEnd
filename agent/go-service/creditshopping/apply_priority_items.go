package creditshopping

import (
	"encoding/json"
	"fmt"
	"slices"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const creditShoppingApplyPriorityItemsActionName = "CreditShoppingApplyPriorityItemsAction"

// ApplyPriorityItemsAction 将任务选项中的多选物品 ID 写入对应 PriorityN 子节点，并按用户顺序重写 Or 的 any_of。
type ApplyPriorityItemsAction struct{}

var _ maa.CustomActionRunner = (*ApplyPriorityItemsAction)(nil)

type applyPriorityItemsParam struct {
	Priority1 []string `json:"priority1,omitempty"`
	Priority2 []string `json:"priority2,omitempty"`
	Priority3 []string `json:"priority3,omitempty"`
}

func (a *ApplyPriorityItemsAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if ctx == nil {
		log.Error().Str("component", component).Msg("apply priority items: nil context")
		return false
	}
	if arg == nil {
		log.Error().Str("component", component).Msg("apply priority items: nil custom action arg")
		return false
	}

	var param applyPriorityItemsParam
	if raw := arg.CustomActionParam; raw != "" {
		if err := json.Unmarshal([]byte(raw), &param); err != nil {
			log.Error().Err(err).Str("component", component).Msg("apply priority items: parse param failed")
			return false
		}
	}

	patch := map[string]any{}
	for level, ids := range map[int][]string{
		1: param.Priority1,
		2: param.Priority2,
		3: param.Priority3,
	} {
		if ids == nil {
			continue
		}
		levelPatch, err := buildPriorityLevelOverride(level, ids)
		if err != nil {
			log.Error().Err(err).Int("priority", level).Str("component", component).Msg("apply priority items: build override failed")
			return false
		}
		for k, v := range levelPatch {
			patch[k] = v
		}
	}

	if len(patch) == 0 {
		log.Debug().Str("component", component).Msg("apply priority items: empty param, skip OverridePipeline")
		return true
	}

	if err := ctx.OverridePipeline(patch); err != nil {
		log.Error().Err(err).Str("component", component).Interface("patch_keys", patchKeys(patch)).Msg("apply priority items: OverridePipeline failed")
		return false
	}

	log.Info().
		Str("component", component).
		Interface("priority1", param.Priority1).
		Interface("priority2", param.Priority2).
		Interface("priority3", param.Priority3).
		Msg("apply priority items: OverridePipeline applied")
	return true
}

func buildPriorityLevelOverride(level int, selectedIDs []string) (map[string]any, error) {
	if level < 1 || level > 3 {
		return nil, fmt.Errorf("invalid priority level %d", level)
	}

	normalized := make([]string, 0, len(selectedIDs))
	seen := make(map[string]struct{}, len(selectedIDs))
	for _, id := range selectedIDs {
		if id == "" {
			continue
		}
		if _, ok := priorityItemByID(id); !ok {
			return nil, fmt.Errorf("unknown item id %q for priority %d", id, level)
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		normalized = append(normalized, id)
	}

	selectedSet := make(map[string]struct{}, len(normalized))
	for _, id := range normalized {
		selectedSet[id] = struct{}{}
	}

	patch := map[string]any{}
	for _, item := range priorityItemCatalog {
		node := priorityItemNodeName(level, item.ID)
		if _, ok := selectedSet[item.ID]; ok {
			patch[node] = enabledPriorityItemOverride(item)
		} else {
			patch[node] = disabledPriorityItemOverride()
		}
	}

	anyOf := make([]string, 0, len(normalized))
	for _, id := range normalized {
		anyOf = append(anyOf, priorityItemNodeName(level, id))
	}
	patch[priorityItemOrNodeName(level)] = map[string]any{
		"recognition": "Or",
		"any_of":      anyOf,
	}

	return patch, nil
}

func patchKeys(patch map[string]any) []string {
	keys := make([]string, 0, len(patch))
	for k := range patch {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}
