package creditshopping

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const (
	creditShoppingApplyPriorityItemsActionName = "CreditShoppingApplyPriorityItemsAction"
	creditShoppingApplyPriorityItemsNodeName   = "CreditShoppingApplyPriorityItems"
)

// ApplyPriorityItemsAction 将任务选项中的多选物品写成各档 IconRecognition 节点的 item_ids。
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
	param = mergeApplyPriorityParamFromAttach(ctx, param)

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

	ordered := orderIDsByCase(normalized)
	if len(ordered) == 0 {
		return map[string]any{}, nil
	}
	return map[string]any{
		priorityItemNodeName(level): priorityItemIconRecognitionOverride(ordered),
	}, nil
}

func orderIDsByCase(ids []string) []string {
	if len(ids) == 0 {
		return nil
	}
	selected := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		selected[id] = struct{}{}
	}
	out := make([]string, 0, len(selected))
	for _, id := range priorityItemCaseOrder {
		if _, ok := selected[id]; ok {
			out = append(out, id)
		}
	}
	return out
}

func mergeApplyPriorityParamFromAttach(ctx *maa.Context, param applyPriorityItemsParam) applyPriorityItemsParam {
	if !applyPriorityParamEmpty(param) {
		return param
	}
	fromAttach, ok := priorityItemsFromAttach(ctx, creditShoppingApplyPriorityItemsNodeName)
	if !ok {
		return param
	}
	return fromAttach
}

func applyPriorityParamEmpty(param applyPriorityItemsParam) bool {
	return len(param.Priority1) == 0 && len(param.Priority2) == 0 && len(param.Priority3) == 0
}

func priorityItemsFromAttach(ctx *maa.Context, nodeName string) (applyPriorityItemsParam, bool) {
	raw, err := ctx.GetNodeJSON(nodeName)
	if err != nil || raw == "" {
		return applyPriorityItemsParam{}, false
	}
	var node struct {
		Attach map[string]json.RawMessage `json:"attach"`
	}
	if err := json.Unmarshal([]byte(raw), &node); err != nil || len(node.Attach) == 0 {
		return applyPriorityItemsParam{}, false
	}

	selected := map[int]map[string]struct{}{
		1: {},
		2: {},
		3: {},
	}
	for key, rawValue := range node.Attach {
		level, itemID, ok := parseAttachPriorityKey(key)
		if !ok || !attachValueEnabled(rawValue) {
			continue
		}
		selected[level][itemID] = struct{}{}
	}

	out := applyPriorityItemsParam{
		Priority1: orderIDsByCase(mapKeys(selected[1])),
		Priority2: orderIDsByCase(mapKeys(selected[2])),
		Priority3: orderIDsByCase(mapKeys(selected[3])),
	}
	if applyPriorityParamEmpty(out) {
		return applyPriorityItemsParam{}, false
	}
	return out, true
}

func parseAttachPriorityKey(key string) (level int, itemID string, ok bool) {
	for lvl := 1; lvl <= 3; lvl++ {
		prefix := fmt.Sprintf("priority%d__", lvl)
		if rest, found := strings.CutPrefix(key, prefix); found && rest != "" {
			return lvl, rest, true
		}
	}
	return 0, "", false
}

func attachValueEnabled(raw json.RawMessage) bool {
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

func mapKeys(set map[string]struct{}) []string {
	if len(set) == 0 {
		return nil
	}
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	return keys
}

func patchKeys(patch map[string]any) []string {
	keys := make([]string, 0, len(patch))
	for k := range patch {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}
