package creditshopping

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

type CreditShoppingParseParams struct{}

func (a *CreditShoppingParseParams) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if arg.CustomActionParam != "" {
		log.Info().Str("custom_action_param", arg.CustomActionParam).Msg("CreditShoppingParseParams input")
	}

	nodeAttachCache := make(map[string]map[string]interface{})
	getNodeAttach := func(nodeName string) map[string]interface{} {
		if attach, ok := nodeAttachCache[nodeName]; ok {
			return attach
		}

		raw, err := ctx.GetNodeJSON(nodeName)
		if err != nil {
			log.Error().Err(err).Str("node", nodeName).Msg("Failed to get node json for attach")
			return nil
		}
		if raw == "" {
			log.Error().Str("node", nodeName).Msg("Node json is empty for attach")
			return nil
		}

		var nodeData map[string]interface{}
		if err := json.Unmarshal([]byte(raw), &nodeData); err != nil {
			log.Error().Err(err).Str("node", nodeName).Msg("Failed to unmarshal node json for attach")
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
				log.Warn().Str("key", key).Interface("value", value).Msg("unsupported attach keyword value type, expect string or string list")
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
			return "^$"
		}
		escaped := make([]string, 0, len(keywords))
		for _, keyword := range keywords {
			escaped = append(escaped, regexp.QuoteMeta(keyword))
		}
		return fmt.Sprintf("^(%s)$", strings.Join(escaped, "|"))
	}

	buildBlacklistRegex := func(keywords []string) string {
		if len(keywords) == 0 {
			return "^.*$"
		}
		escaped := make([]string, 0, len(keywords))
		for _, keyword := range keywords {
			escaped = append(escaped, regexp.QuoteMeta(keyword))
		}
		return fmt.Sprintf("^(?!(%s)$).*$", strings.Join(escaped, "|"))
	}

	buyFirstKeywords := mergeKeywordLists(
		collectKeywords(getNodeAttach("BuyFirstOCR")),
		collectKeywords(getNodeAttach("BuyFirstOCR_CanNotAfford")),
	)
	blacklistKeywords := collectKeywords(getNodeAttach("BlacklistOCR"))

	buyFirstExpected := buildWhitelistRegex(buyFirstKeywords)
	blacklistExpected := buildBlacklistRegex(blacklistKeywords)

	log.Info().
		Interface("buy_first_keywords", buyFirstKeywords).
		Interface("blacklist_keywords", blacklistKeywords).
		Str("buy_first_expected", buyFirstExpected).
		Str("blacklist_expected", blacklistExpected).
		Msg("CreditShoppingParseParams merged from attach")

	overrideMap := map[string]interface{}{
		"BuyFirstOCR": map[string]interface{}{
			"expected": buyFirstExpected,
		},
		"BuyFirstOCR_CanNotAfford": map[string]interface{}{
			"expected": buyFirstExpected,
		},
		"BlacklistOCR": map[string]interface{}{
			"expected": blacklistExpected,
		},
	}

	log.Info().Interface("override", overrideMap).Msg("CreditShoppingParseParams override")

	if err := ctx.OverridePipeline(overrideMap); err != nil {
		log.Error().Err(err).Interface("override", overrideMap).Msg("Failed to OverridePipeline")
		return false
	}

	return true
}
