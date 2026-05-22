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

	stage, err := parseStage(arg.CustomActionParam)
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

	switch stage {
	case stageInit:
		return handleInit(ctx)
	case stageRecordOriginium:
		return handleRecordResource(ctx, arg, true)
	case stageRecordOroberyl:
		return handleRecordResource(ctx, arg, false)
	case stageRecordVoucher:
		return handleRecordVoucher(ctx, arg)
	case stageFinish:
		return handleFinish(ctx)
	default:
		log.Error().Str("component", componentName).Str("stage", stage).Msg("unknown stage")
		maafocus.Print(ctx, i18n.T("pullcount.error.invalid_params"))
		return false
	}
}

// parseStage reads the pull-count stage name passed from Pipeline.
func parseStage(raw string) (string, error) {
	var param struct {
		Stage string `json:"stage"`
	}
	if strings.TrimSpace(raw) != "" {
		if err := json.Unmarshal([]byte(raw), &param); err != nil {
			return "", err
		}
	}

	return strings.TrimSpace(param.Stage), nil
}
