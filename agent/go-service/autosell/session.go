package autosell

import (
	"encoding/json"
	"strings"
	"sync"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/ocrnum"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

type itemState struct {
	Name     string
	Category int
	OwnPrice int
	Sold     bool
}

type regionState struct {
	Items  []itemState
	Prices [3]int
}

type sessionState struct {
	Regions map[string]*regionState
	Region  string
	Index   int
}

var (
	sessionMu sync.Mutex
	session   = sessionState{Regions: make(map[string]*regionState)}
)

// SessionAction 只维护地区、商品游标、本家价格和已售记录；界面操作与轮次由 Pipeline 驱动。
type SessionAction struct{}

type sessionParam struct {
	Operation     string `json:"operation"`
	Region        string `json:"region"`
	ModeratePrice *int   `json:"moderate_price"`
	LargePrice    *int   `json:"large_price"`
	MassivePrice  *int   `json:"massive_price"`
}

var _ maa.CustomActionRunner = (*SessionAction)(nil)

func (a *SessionAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if ctx == nil || arg == nil {
		return false
	}
	var param sessionParam
	if err := json.Unmarshal([]byte(arg.CustomActionParam), &param); err != nil {
		log.Error().Err(err).Str("component", "AutoSellSession").Msg("invalid params")
		return false
	}
	if param.Operation == "begin" {
		if param.Region != "ValleyIV" && param.Region != "Wuling" {
			log.Error().Str("component", "AutoSellSession").Str("region", param.Region).Msg("invalid region")
			return false
		}
		for _, price := range []*int{param.ModeratePrice, param.LargePrice, param.MassivePrice} {
			if price != nil && *price < 0 {
				log.Error().Str("component", "AutoSellSession").Msg("negative price threshold")
				return false
			}
		}
		if err := ctx.OverridePipeline(regionOverrides(param.Region)); err != nil {
			log.Error().Err(err).Str("component", "AutoSellSession").Msg("configure region nodes")
			return false
		}
	}

	sessionMu.Lock()
	defer sessionMu.Unlock()
	switch param.Operation {
	case "reset":
		session = sessionState{Regions: make(map[string]*regionState)}
		return true
	case "begin":
		beginRegion(param)
		return true
	case "save_own_price", "advance", "sold":
		region := session.Regions[session.Region]
		if region == nil || session.Index >= len(region.Items) {
			log.Error().Str("component", "AutoSellSession").Str("operation", param.Operation).Msg("no selected item")
			return false
		}
		item := &region.Items[session.Index]
		switch param.Operation {
		case "save_own_price":
			price, err := ocrnum.Extract(arg.RecognitionDetail)
			if err != nil || price < 0 {
				price = 0
				log.Warn().Err(err).Str("component", "AutoSellSession").
					Str("region", session.Region).Str("item_name", item.Name).
					Msg("own price unavailable, use zero for friend comparison")
			}
			item.OwnPrice = price
			log.Info().Str("component", "AutoSellSession").Str("region", session.Region).
				Str("item_name", item.Name).Int("own_price", price).Msg("saved own price")
		case "advance":
			session.Index++
		case "sold":
			item.Sold = true
			log.Info().Str("component", "AutoSellSession").Str("region", session.Region).
				Str("item_name", item.Name).Msg("confirmed item sold")
		}
		return true
	default:
		log.Error().Str("component", "AutoSellSession").Str("operation", param.Operation).Msg("invalid operation")
		return false
	}
}

func beginRegion(param sessionParam) {
	region := session.Regions[param.Region]
	if region == nil {
		region = newRegionState(nil)
		session.Regions[param.Region] = region
	}
	for index, price := range []*int{param.ModeratePrice, param.LargePrice, param.MassivePrice} {
		if price != nil {
			region.Prices[index] = *price
		}
	}
	session.Region = param.Region
	session.Index = 0
}

func newRegionState(names []string) *regionState {
	region := &regionState{Prices: [3]int{4600, 5000, 5300}}
	seen := make(map[string]struct{}, len(names))
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if _, duplicate := seen[name]; duplicate {
			continue
		}
		seen[name] = struct{}{}
		category := -1
		for index, keywords := range [][]string{moderatePriceKeywords, largePriceKeywords, massivePriceKeywords} {
			if firstContainedKeyword(name, keywords) != "" {
				category = index
				break
			}
		}
		region.Items = append(region.Items, itemState{Name: name, Category: category})
	}
	return region
}

func regionOverrides(region string) map[string]any {
	override := make(map[string]any)
	for _, name := range []string{"ValleyIV", "Wuling"} {
		for _, prefix := range []string{
			"AutoSellStockRedistributionItemOpenPrepareRegionalDevelopment",
			"AutoSellStockRedistributionItemOpenPrepareFriendsSwitch",
			"AutoSellStockRedistributionItemOpenPrepareFriendsFailedTo",
		} {
			override[prefix+name] = map[string]any{"enabled": region == name}
		}
	}
	return override
}
