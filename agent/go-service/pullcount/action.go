package pullcount

import (
	"encoding/json"
	"strings"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/maafocus"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const (
	componentName = "PullCountCalculator"

	stageInit            = "init"
	stageRecordOriginium = "record_originium"
	stageRecordOroberyl  = "record_oroberyl"
	stageRecordVoucher   = "record_voucher"
	stagePageDone        = "page_done"
	stageFinish          = "finish"

	reservedOriginium   = 29
	originiumToOroberyl = 75
	oroberylPerPull     = 500
	nextPoolShopPulls   = 5
	nextPoolSigninPulls = 5
)

var _ maa.CustomActionRunner = &Action{}

// Action calculates current and next-version recruitment pulls from Pipeline-provided OCR results.
type Action struct{}

// --- Entry And Parameters --- //

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

	param, err := parseActionParam(arg.CustomActionParam)
	if err != nil {
		log.Error().
			Err(err).
			Str("component", componentName).
			Str("custom_action_param", arg.CustomActionParam).
			Msg("failed to parse action params")
		maafocus.Print(ctx, i18n.T("pullcount.error.invalid_params"))
		return false
	}

	stage := resolveStage(param.Stage, arg.CurrentTaskName)
	sessionMu.Lock()
	defer sessionMu.Unlock()

	switch stage {
	case stageInit:
		return handleInit(ctx)
	case stageRecordOriginium:
		return handleRecordResource(ctx, arg, true)
	case stageRecordOroberyl:
		return handleRecordResource(ctx, arg, false)
	case stageRecordVoucher:
		return handleRecordVoucher(ctx, arg, param)
	case stagePageDone:
		return handlePageDone(ctx)
	case stageFinish:
		return handleFinish(ctx)
	default:
		log.Error().Str("component", componentName).Str("stage", stage).Msg("unknown stage")
		maafocus.Print(ctx, i18n.T("pullcount.error.invalid_params"))
		return false
	}
}

// parseActionParam parses the small per-node behavior parameters passed from Pipeline.
func parseActionParam(raw string) (*actionParam, error) {
	var param actionParam
	if strings.TrimSpace(raw) != "" {
		if err := json.Unmarshal([]byte(raw), &param); err != nil {
			return nil, err
		}
	}

	param.Stage = strings.TrimSpace(param.Stage)
	param.PoolScope = strings.TrimSpace(param.PoolScope)
	return &param, nil
}

// resolveStage keeps the old main-node entry compatible with an empty stage parameter.
func resolveStage(stage string, currentTaskName string) string {
	stage = strings.TrimSpace(stage)
	if stage != "" {
		return stage
	}
	if currentTaskName == "PullCountCalculatorMain" {
		return stageInit
	}
	return ""
}
