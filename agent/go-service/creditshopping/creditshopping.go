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
			log.Error().Str("node", nodeName).Msg("attach field not found or invalid")
			return nil
		}

		nodeAttachCache[nodeName] = attachRaw
		return attachRaw
	}

	onlyBuyDiscount := false
	var discount2OCROffset []int
	if attach := getNodeAttach("CreditShoppingBuyNormal"); attach != nil {
		if v, ok := attach["only_buy_discount"]; ok {
			if b, ok := v.(bool); ok {
				onlyBuyDiscount = b
			}
		}
		if v, ok := attach["offset"]; ok {
			if arr, ok := v.([]interface{}); ok && len(arr) == 4 {
				tmp := make([]int, 4)
				valid := true
				for i, val := range arr {
					switch n := val.(type) {
					case float64:
						tmp[i] = int(n)
					case int:
						tmp[i] = n
					default:
						log.Error().Interface("offset", v).Msg("invalid offset element type, expect number")
						valid = false
					}
				}
				if valid {
					discount2OCROffset = tmp
				}
			} else {
				log.Error().Interface("offset", v).Msg("offset field not valid, expect [x,y,w,h]")
			}
		}
	}
	log.Info().Bool("only_buy_discount", onlyBuyDiscount).Msg("CreditShoppingParseParams flag")

	// 3. Get all_of from attach, replace expected, and write back to override all_of
	overrideMap := map[string]interface{}{}

	// Helper: get attach.all_of from node json（所有节点统一通过 getNodeAttach）
	getAllOfFromAttach := func(nodeName string) ([]interface{}, bool) {
		attach := getNodeAttach(nodeName)
		if attach == nil {
			return nil, false
		}

		allOf, ok := attach["all_of"].([]interface{})
		if !ok {
			log.Error().Str("node", nodeName).Msg("attach.all_of field not found or invalid")
			return nil, false
		}

		return allOf, true
	}

	if allOf, ok := getAllOfFromAttach("CreditShoppingBuyFirst"); ok {
		if len(buyFirstExpected) > 0 {
			for _, item := range allOf {
				itemMap, ok := item.(map[string]interface{})
				if !ok {
					continue
				}
				subName, _ := itemMap["sub_name"].(string)
				if subName == "BuyFirstOCR" {
					itemMap["expected"] = buyFirstExpected
					break
				}
			}
		}

		overrideMap["CreditShoppingBuyFirst"] = map[string]interface{}{
			"all_of":    allOf,
			"box_index": len(allOf) - 1,
		}
	}

	if allOf, ok := getAllOfFromAttach("CreditShoppingBuyNormal"); ok {
		// Track position after NotSoldOut for potential discount subrec insertion
		insertIdx := -1

		for idx, item := range allOf {
			itemMap, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			subName, _ := itemMap["sub_name"].(string)
			if subName == "NotSoldOut" {
				insertIdx = idx + 1
			}
			if subName == "BlacklistOCR" {
				if len(blacklistExpected) > 0 {
					itemMap["expected"] = blacklistExpected
				}
				if onlyBuyDiscount {
					itemMap["roi"] = "IsDiscount"
					// 若配置了 offset，则同时调整 BlacklistOCR 的 roi_offset
					if len(discount2OCROffset) == 4 {
						itemMap["roi_offset"] = discount2OCROffset
					}
				}
			}
		}

		// If only_buy_discount is enabled, insert discount sub-recognition after NotSoldOut
		if onlyBuyDiscount && insertIdx >= 0 {
			if attach := getNodeAttach("CreditShoppingBuyNormal"); attach != nil {
				if subrec, ok := attach["only_buy_discount_subrec"].(map[string]interface{}); ok {
					newAllOf := make([]interface{}, 0, len(allOf)+1)
					newAllOf = append(newAllOf, allOf[:insertIdx]...)
					newAllOf = append(newAllOf, subrec)
					newAllOf = append(newAllOf, allOf[insertIdx:]...)
					allOf = newAllOf
				} else {
					log.Error().Str("node", "CreditShoppingBuyNormal").Msg("only_buy_discount_subrec field not found or invalid")
				}
			}
		}

		overrideMap["CreditShoppingBuyNormal"] = map[string]interface{}{
			"all_of":    allOf,
			"box_index": len(allOf) - 1,
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
