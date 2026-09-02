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

	stageInit   = "init"
	stageRecord = "record"
	stageFinish = "finish"

	reservedOriginium   = 29
	originiumToOroberyl = 75
	oroberylPerPull     = 500
	nextPoolShopPulls   = 5
	nextPoolSigninPulls = 5
)

var _ maa.CustomActionRunner = &Action{}

// actionParam is custom_action_param for PullCountCalculatorAction.
//
// Pipeline owns navigation. This action only reads quantities and calculates:
//   - init: start a session
//   - record: run items (item ID → And recognizer node, box_index → OCR digit)
//   - finish: compute current / next-pool pulls from session, falling back to IMS
//
// item_originium_recharge is the raw 衍质源石 count (not the converted 嵌晶玉 display).
type actionParam struct {
	Stage string `json:"stage"`
	// Items maps catalog / IMS item ID → Pipeline recognition node name.
	Items map[string]string `json:"items"`
	// Optional when true: a miss stores 0 instead of failing (珍贵物品券).
	// Default false: currencies must hit.
	Optional bool `json:"optional"`
}

// Action reads And-recognizer quantities and calculates recruitment pulls.
type Action struct{}

// Run dispatches one Pipeline stage of the pull-count calculation.
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

	sessionMu.Lock()
	defer sessionMu.Unlock()

	switch params.Stage {
	case stageInit:
		return handleInit(ctx)
	case stageRecord:
		return handleRecord(ctx, params)
	case stageFinish:
		return handleFinish(ctx)
	default:
		log.Error().Str("component", componentName).Str("stage", params.Stage).Msg("unknown stage")
		maafocus.Print(ctx, i18n.T("pullcount.error.invalid_params"))
		return false
	}
}

func parseActionParam(raw string) (actionParam, error) {
	var params actionParam
	if strings.TrimSpace(raw) == "" {
		return actionParam{}, fmt.Errorf("custom_action_param is required")
	}
	if err := json.Unmarshal([]byte(raw), &params); err != nil {
		return actionParam{}, err
	}
	params.Stage = strings.TrimSpace(params.Stage)
	if params.Stage == "" {
		return actionParam{}, fmt.Errorf("stage is required")
	}
	items, err := normalizeItemsMap(params.Items)
	if err != nil {
		return actionParam{}, err
	}
	params.Items = items
	if params.Stage == stageRecord && len(params.Items) == 0 {
		return actionParam{}, fmt.Errorf("record stage requires items")
	}
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
