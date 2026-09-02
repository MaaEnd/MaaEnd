package pullcount

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/maafocus"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const (
	componentName = "PullCountCalculator"

	reservedOriginium   = 29
	originiumToOroberyl = 75
	oroberylPerPull     = 500
	nextPoolShopPulls   = 5
	nextPoolSigninPulls = 5
)

var _ maa.CustomActionRunner = &Action{}

// actionParam is custom_action_param for PullCountCalculatorAction.
//
// Call on the 珍贵物品 page. 嵌晶玉 / 衍质源石 come from IMS.
// items (optional) are And recognizers on the current screen (box_index → OCR digit).
// Do not pass item_diamond / item_originium_recharge in items.
type actionParam struct {
	Items map[string]string `json:"items"`
	// Optional when true: a miss stores 0 instead of failing. Default true.
	Optional *bool `json:"optional"`
}

// Action recognizes on-screen gacha vouchers and calculates recruitment pulls.
type Action struct{}

// Run reads IMS currencies, optionally runs voucher recognizers, then prints the pull count.
func (a *Action) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if ctx == nil {
		log.Error().Str("component", componentName).Msg("context is nil")
		return false
	}
	if arg == nil {
		log.Error().Str("component", componentName).Msg("custom action arg is nil")
		return false
	}

	params, err := parseActionParam(arg.CustomActionParam)
	if err != nil {
		log.Error().
			Err(err).
			Str("component", componentName).
			Str("custom_action_param", arg.CustomActionParam).
			Msg("failed to parse action params")
		maafocus.Print(ctx, i18n.T("pullcount.error.invalid_params"))
		return false
	}

	return handleCalculate(ctx, params)
}

func parseActionParam(raw string) (actionParam, error) {
	var params actionParam
	if strings.TrimSpace(raw) == "" || raw == "{}" {
		return params, nil
	}
	if err := json.Unmarshal([]byte(raw), &params); err != nil {
		return actionParam{}, err
	}
	items, err := normalizeItemsMap(params.Items)
	if err != nil {
		return actionParam{}, err
	}
	params.Items = items
	return params, nil
}

func normalizeItemsMap(items map[string]string) (map[string]string, error) {
	if len(items) == 0 {
		return items, nil
	}
	out := make(map[string]string, len(items))
	for id, node := range items {
		id = strings.TrimSpace(id)
		node = strings.TrimSpace(node)
		if id == "" || node == "" {
			return nil, fmt.Errorf("items contains empty item id or node name")
		}
		if _, dup := out[id]; dup {
			return nil, fmt.Errorf("items contains duplicate item id after trim: %s", id)
		}
		out[id] = node
	}
	return out, nil
}

func resolveOptional(v *bool) bool {
	if v == nil {
		return true
	}
	return *v
}
