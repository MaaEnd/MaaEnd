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

// ApplyPriorityItemsAction 把任务选项的勾选（节点 attach）复制成各档 IconRecognition 节点的 item_ids。
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

	selected := priorityItemsFromAttach(ctx, creditShoppingApplyPriorityItemsNodeName)
	if len(selected) == 0 {
		log.Debug().Str("component", component).Msg("apply priority items: empty selection, skip OverridePipeline")
		return true
	}

	patch := map[string]any{}
	for level, ids := range selected {
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
		log.Debug().Str("component", component).Msg("apply priority items: empty patch, skip OverridePipeline")
		return true
	}

	if err := ctx.OverridePipeline(patch); err != nil {
		log.Error().Err(err).Str("component", component).Interface("patch_keys", patchKeys(patch)).Msg("apply priority items: OverridePipeline failed")
		return false
	}

	log.Info().
		Str("component", component).
		Interface("priority1", selected[1]).
		Interface("priority2", selected[2]).
		Interface("priority3", selected[3]).
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

	if len(normalized) == 0 {
		return map[string]any{}, nil
	}
	return map[string]any{
		priorityItemNodeName(level): priorityItemIconRecognitionOverride(normalized),
	}, nil
}

// priorityItemsFromAttach 读取节点 attach 里 priority{N}__<CaseID> 的勾选项，按档位分组返回。
func priorityItemsFromAttach(ctx *maa.Context, nodeName string) map[int][]string {
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

	selected := map[int][]string{}
	for key, rawValue := range node.Attach {
		level, itemID, ok := parseAttachPriorityKey(key)
		if !ok || !attachValueEnabled(rawValue) {
			continue
		}
		selected[level] = append(selected[level], itemID)
	}
	if len(selected) == 0 {
		return nil
	}
	return selected
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

func patchKeys(patch map[string]any) []string {
	keys := make([]string, 0, len(patch))
	for k := range patch {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}
