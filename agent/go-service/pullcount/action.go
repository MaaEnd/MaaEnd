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

	stageInit            = "init"
	stageRecordOriginium = "record_originium"
	stageRecordOroberyl  = "record_oroberyl"
	stageRecordQuantity  = "record_quantity"
	stageRecordVoucher   = "record_voucher"
	stagePageBegin       = "page_begin"
	stagePageDone        = "page_done"
	stageProbeBegin      = "probe_begin"
	stageRecordProbe     = "record_probe_quantity"
	stageFinish          = "finish"

	stagePageShouldFinish     = "page_should_finish"
	stageScrollProbeUnchanged = "scroll_probe_unchanged"
)

var _ maa.CustomActionRunner = &Action{}

var defaultActionParam = actionParam{
	ReservedOriginium:   29,
	OriginiumToOroberyl: 75,
	OroberylPerPull:     500,
	NextPoolShopPulls:   5,
	NextPoolSigninPulls: 5,
	Probe:               warehouseSimilarityRule{CellLimit: 9, MinComparable: 4, MaxMismatches: 1, MinMatchRatio: 0.85},
	RepeatPage:          warehouseSimilarityRule{CellLimit: 45, MinComparable: 8, MaxMismatches: 1, MinMatchRatio: 0.85},
	ScanMaxPages:        8,
}

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
		return handleInit(ctx, param)
	case stageRecordOriginium:
		return handleRecordResource(ctx, arg, true)
	case stageRecordOroberyl:
		return handleRecordResource(ctx, arg, false)
	case stageRecordQuantity:
		return handleQuantityOCR(ctx, arg, param.Cell, false)
	case stageRecordVoucher:
		return handleRecordVoucher(ctx, param)
	case stagePageBegin:
		return handleScanBegin(ctx, false)
	case stagePageDone:
		return handlePageDone(ctx)
	case stageProbeBegin:
		return handleScanBegin(ctx, true)
	case stageRecordProbe:
		return handleQuantityOCR(ctx, arg, param.Cell, true)
	case stageFinish:
		return handleFinish(ctx)
	default:
		log.Error().Str("component", componentName).Str("stage", stage).Msg("unknown stage")
		maafocus.Print(ctx, i18n.T("pullcount.error.invalid_params"))
		return false
	}
}

// parseActionParam parses stage parameters and fills default calculation constants.
func parseActionParam(raw string) (*actionParam, error) {
	param := defaultActionParam
	if strings.TrimSpace(raw) != "" {
		if err := json.Unmarshal([]byte(raw), &param); err != nil {
			return nil, err
		}
	}

	param.Stage = strings.TrimSpace(param.Stage)
	param.PoolScope = strings.TrimSpace(param.PoolScope)
	if param.OriginiumToOroberyl <= 0 || param.OroberylPerPull <= 0 {
		return nil, fmt.Errorf("resource conversion constants must be positive")
	}
	if param.NextPoolShopPulls < 0 || param.NextPoolSigninPulls < 0 {
		return nil, fmt.Errorf("next pool fixed pulls must be non-negative")
	}
	scanConfig := param.scanConfig()
	if err := scanConfig.validate(); err != nil {
		return nil, err
	}
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
