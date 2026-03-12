package creditshopping

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

// CreditShoppingParseParams reads shopping configuration from node attach data and applies
// pipeline overrides for OCR matching. It caches the computed override per pipeline context
// to avoid redundant re-initialization when triggered multiple times within the same loop.
type CreditShoppingParseParams struct {
	mu      sync.Mutex
	lastCtx *maa.Context
}

var _ maa.CustomActionRunner = &CreditShoppingParseParams{}

func (a *CreditShoppingParseParams) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	// Hold the lock for the entire Run to prevent duplicate initialization
	// when the action is triggered concurrently. Within a single pipeline run,
	// MaaFramework passes the same ctx pointer across loop iterations, so
	// pointer equality reliably identifies "already initialized for this run".
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.lastCtx == ctx {
		log.Debug().Str("component", "CreditShopping").Msg("pipeline override already applied, skipping")
		return true
	}

	if arg.CustomActionParam != "" {
		log.Info().Str("component", "CreditShopping").Str("custom_action_param", arg.CustomActionParam).Msg("input received")
	}

	nodeAttachCache := make(map[string]map[string]interface{})
	getNodeAttach := func(nodeName string) map[string]interface{} {
		if attach, ok := nodeAttachCache[nodeName]; ok {
			return attach
		}

		raw, err := ctx.GetNodeJSON(nodeName)
		if err != nil {
			log.Error().Err(err).Str("component", "CreditShopping").Str("node", nodeName).Msg("failed to get node json for attach")
			return nil
		}
		if raw == "" {
			log.Error().Str("component", "CreditShopping").Str("node", nodeName).Msg("node json is empty for attach")
			return nil
		}

		var nodeData map[string]interface{}
		if err := json.Unmarshal([]byte(raw), &nodeData); err != nil {
			log.Error().Err(err).Str("component", "CreditShopping").Str("node", nodeName).Msg("failed to unmarshal node json for attach")
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
				log.Warn().Str("component", "CreditShopping").Str("key", key).Interface("value", value).Msg("unsupported attach keyword value type, expect string or string list")
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

	log.Debug().
		Str("component", "CreditShopping").
		Interface("buy_first_keywords", buyFirstKeywords).
		Interface("blacklist_keywords", blacklistKeywords).
		Str("buy_first_expected", buyFirstExpected).
		Str("blacklist_expected", blacklistExpected).
		Msg("merged keywords from attach")

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

	log.Debug().
		Str("component", "CreditShopping").
		Interface("override", overrideMap).
		Msg("applying pipeline override")

	if err := ctx.OverridePipeline(overrideMap); err != nil {
		log.Error().Err(err).Str("component", "CreditShopping").Interface("override", overrideMap).Msg("OverridePipeline failed")
		return false
	}

	a.lastCtx = ctx
	return true
}
