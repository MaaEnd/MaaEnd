package creditshopping

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

type CreditShoppingParseParams struct{}

func (a *CreditShoppingParseParams) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	var params struct {
		BuyFirst  string `json:"buy_first"`
		Blacklist string `json:"blacklist"`
	}

	if err := json.Unmarshal([]byte(arg.CustomActionParam), &params); err != nil {
		log.Error().Err(err).Msg("Failed to parse CustomActionParam")
		return false
	}

	log.Info().Str("buy_first", params.BuyFirst).Str("blacklist", params.Blacklist).Msg("CreditShoppingParseParams input")

	// 1. Process BuyFirst
	// Convert "A;B" -> ["A", "B"]
	var buyFirstExpected []string
	if params.BuyFirst != "" {
		parts := strings.Split(params.BuyFirst, ";")
		for _, part := range parts {
			if trimmed := strings.TrimSpace(part); trimmed != "" {
				buyFirstExpected = append(buyFirstExpected, trimmed)
			}
		}
	}

	log.Info().Interface("buy_first", buyFirstExpected).Msg("CreditShoppingParseParams buy_first")

	// 2. Process Blacklist
	// Convert "A;B" -> ["^(?!.*A)(?!.*B).*$"]
	var blacklistExpected []string
	if params.Blacklist != "" {
		parts := strings.Split(params.Blacklist, ";")
		var sb strings.Builder
		sb.WriteString("^")
		for _, part := range parts {
			if trimmed := strings.TrimSpace(part); trimmed != "" {
				// Pattern: (?!.*KEYWORD)
				quoted := regexp.QuoteMeta(trimmed)
				sb.WriteString(fmt.Sprintf("(?!(?:.*%s))", quoted))
			}
		}
		sb.WriteString(".*$")
		blacklistExpected = append(blacklistExpected, sb.String())
	}

	log.Info().Interface("blacklist", blacklistExpected).Msg("CreditShoppingParseParams blacklist")

	// 3. Get all_of from attach, replace expected, and write back to override all_of
	overrideMap := map[string]interface{}{}

	// Helper: get attach.all_of from node json
	getAllOfFromAttach := func(nodeName string) ([]interface{}, bool) {
		raw, err := ctx.GetNodeJSON(nodeName)
		if err != nil {
			log.Error().Err(err).Str("node", nodeName).Msg("Failed to get node json")
			return nil, false
		}
		if raw == "" {
			log.Error().Str("node", nodeName).Msg("Node json is empty")
			return nil, false
		}

		var nodeData map[string]interface{}
		if err := json.Unmarshal([]byte(raw), &nodeData); err != nil {
			log.Error().Err(err).Str("node", nodeName).Msg("Failed to unmarshal node json")
			return nil, false
		}

		attachRaw, ok := nodeData["attach"].(map[string]interface{})
		if !ok {
			log.Error().Str("node", nodeName).Msg("attach field not found or invalid")
			return nil, false
		}

		allOf, ok := attachRaw["all_of"].([]interface{})
		if !ok {
			log.Error().Str("node", nodeName).Msg("attach.all_of field not found or invalid")
			return nil, false
		}

		return allOf, true
	}

	if len(buyFirstExpected) > 0 {
		allOf, ok := getAllOfFromAttach("CreditShoppingBuyFirst")
		if ok {
			for _, item := range allOf {
				itemMap, ok := item.(map[string]interface{})
				if !ok {
					continue
				}
				subName, _ := itemMap["sub_name"].(string)
				if subName == "BuyFirstOCR" {
					// expected is a regex array
					itemMap["expected"] = buyFirstExpected
					break
				}
			}

			overrideMap["CreditShoppingBuyFirst"] = map[string]interface{}{
				"all_of":    allOf,
				"box_index": len(allOf) - 1,
			}
		}
	}

	if len(blacklistExpected) > 0 {
		allOf, ok := getAllOfFromAttach("CreditShoppingBuyNormal")
		if ok {
			for _, item := range allOf {
				itemMap, ok := item.(map[string]interface{})
				if !ok {
					continue
				}
				subName, _ := itemMap["sub_name"].(string)
				if subName == "BlacklistOCR" {
					itemMap["expected"] = blacklistExpected
					break
				}
			}

			overrideMap["CreditShoppingBuyNormal"] = map[string]interface{}{
				"all_of":    allOf,
				"box_index": len(allOf) - 1,
			}
		}
	}

	if len(overrideMap) == 0 {
		return true
	}

	log.Info().Interface("override", overrideMap).Msg("CreditShoppingParseParams override")

	if err := ctx.OverridePipeline(overrideMap); err != nil {
		log.Error().Err(err).Interface("override", overrideMap).Msg("Failed to OverridePipeline")
		return false
	}

	return true
}
