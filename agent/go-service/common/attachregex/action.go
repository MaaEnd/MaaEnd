package attachregex

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

type attachToExpectedRegexParam struct {
	Target string `json:"target"`
}

// AttachToExpectedRegexAction merges attach keywords from the target node itself
// and writes generated regex into the target node's expected field.
type AttachToExpectedRegexAction struct{}

var _ maa.CustomActionRunner = &AttachToExpectedRegexAction{}

func (a *AttachToExpectedRegexAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	var param attachToExpectedRegexParam
	if err := json.Unmarshal([]byte(arg.CustomActionParam), &param); err != nil {
		log.Error().
			Err(err).
			Str("component", "AttachToExpectedRegexAction").
			Str("custom_action_param", arg.CustomActionParam).
			Msg("failed to parse custom action param")
		return false
	}

	if strings.TrimSpace(param.Target) == "" {
		log.Error().
			Str("component", "AttachToExpectedRegexAction").
			Interface("param", param).
			Msg("target is required")
		return false
	}

	return applyAttachRegexOverride(ctx, param.Target, "AttachToExpectedRegexAction")
}

func applyAttachRegexOverride(ctx *maa.Context, targetNodeName string, component string) bool {
	nodeAttachCache := make(map[string]map[string]interface{})
	getNodeAttach := func(nodeName string) map[string]interface{} {
		if attach, ok := nodeAttachCache[nodeName]; ok {
			return attach
		}

		raw, err := ctx.GetNodeJSON(nodeName)
		if err != nil {
			log.Error().Err(err).Str("component", component).Str("node", nodeName).Msg("failed to get node json for attach")
			return nil
		}
		if raw == "" {
			log.Error().Str("component", component).Str("node", nodeName).Msg("node json is empty for attach")
			return nil
		}

		var nodeData map[string]interface{}
		if err := json.Unmarshal([]byte(raw), &nodeData); err != nil {
			log.Error().Err(err).Str("component", component).Str("node", nodeName).Msg("failed to unmarshal node json for attach")
			return nil
		}

		attachRaw, ok := nodeData["attach"].(map[string]interface{})
		if !ok {
			nodeAttachCache[nodeName] = map[string]interface{}{}
			return nodeAttachCache[nodeName]
		}

		nodeAttachCache[nodeName] = attachRaw
		return attachRaw
	}

	collectKeywords := func(attach map[string]interface{}) []string {
		if attach == nil {
			return nil
		}
		keys := make([]string, 0)
		for key := range attach {
			keys = append(keys, key)
		}
		sort.Strings(keys)

		result := make([]string, 0, len(keys))
		for _, key := range keys {
			value := attach[key]
			switch v := value.(type) {
			case string:
				if trimmed := strings.TrimSpace(v); trimmed != "" {
					result = append(result, trimmed)
				}
			case bool:
				// false means explicitly exclude this attach key; true currently adds no keywords.
				if v {
					continue
				}
				continue
			case []interface{}:
				for _, item := range v {
					if s, ok := item.(string); ok {
						if trimmed := strings.TrimSpace(s); trimmed != "" {
							result = append(result, trimmed)
						}
					}
				}
			case []string:
				for _, item := range v {
					if trimmed := strings.TrimSpace(item); trimmed != "" {
						result = append(result, trimmed)
					}
				}
			default:
				log.Warn().Str("component", component).Str("key", key).Interface("value", value).Msg("unsupported attach keyword value type, expect string, string[], or bool(false)")
			}
		}
		return result
	}

	mergeKeywordLists := func(lists ...[]string) []string {
		seen := make(map[string]struct{})
		merged := make([]string, 0)
		for _, list := range lists {
			for _, keyword := range list {
				quoted := strings.TrimSpace(keyword)
				if quoted == "" {
					continue
				}
				if _, ok := seen[quoted]; ok {
					continue
				}
				seen[quoted] = struct{}{}
				merged = append(merged, quoted)
			}
		}
		return merged
	}

	buildWhitelistRegex := func(keywords []string) string {
		if len(keywords) == 0 {
			return "a^"
		}
		escaped := make([]string, 0, len(keywords))
		for _, keyword := range keywords {
			escaped = append(escaped, regexp.QuoteMeta(keyword))
		}
		return fmt.Sprintf("^(%s)$", strings.Join(escaped, "|"))
	}

	keywords := collectKeywords(getNodeAttach(targetNodeName))
	expected := buildWhitelistRegex(mergeKeywordLists(keywords))
	overrideMap := map[string]interface{}{
		targetNodeName: map[string]interface{}{
			"expected": expected,
		},
	}

	log.Debug().
		Str("component", component).
		Str("target", targetNodeName).
		Str("expected", expected).
		Msg("merged keywords from attach")

	log.Debug().
		Str("component", component).
		Interface("override", overrideMap).
		Msg("applying pipeline override")

	if err := ctx.OverridePipeline(overrideMap); err != nil {
		log.Error().Err(err).Str("component", component).Interface("override", overrideMap).Msg("OverridePipeline failed")
		return false
	}

	return true
}
