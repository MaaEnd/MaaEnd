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

	defaultVoucherConfigPath = "data/PullCountCalculator/vouchers.json"
	defaultWarehouseScanPath = "data/PullCountCalculator/warehouse_scan.json"

	stageInit            = "init"
	stageRecordOriginium = "record_originium"
	stageRecordOroberyl  = "record_oroberyl"
	stageRecordQuantity  = "record_quantity"
	stageRecordItem      = "record_item"
	stagePageBegin       = "page_begin"
	stageCellPrepare     = "cell_prepare"
	stageCellAdvance     = "cell_advance"
	stagePageDone        = "page_done"
	stageProbeBegin      = "probe_begin"
	stageProbePrepare    = "probe_prepare"
	stageProbeAdvance    = "probe_advance"
	stageRecordProbe     = "record_probe_quantity"
	stageScrollProbeDone = "scroll_probe_done"
	stageFinish          = "finish"

	nextWarehouseScrollNode = "PullCountCalculatorWarehouseScrollDown"
	nextPageBeginNode       = "PullCountCalculatorPageBegin"
	nextFinishNode          = "PullCountCalculatorFinish"
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
		return handleInit(ctx, param)
	case stageRecordOriginium:
		return handleRecordResource(ctx, arg, true)
	case stageRecordOroberyl:
		return handleRecordResource(ctx, arg, false)
	case stageRecordQuantity:
		return handleRecordQuantity(ctx, arg, param.Cell)
	case stageRecordItem:
		return handleRecordItem(ctx, arg, param.Cell)
	case stagePageBegin:
		return handlePageBegin(ctx)
	case stageCellPrepare:
		return handleCellPrepare(ctx, arg)
	case stageCellAdvance:
		return handleCellAdvance(ctx, arg)
	case stagePageDone:
		return handlePageDone(ctx)
	case stageProbeBegin:
		return handleProbeBegin(ctx)
	case stageProbePrepare:
		return handleProbePrepare(ctx, arg)
	case stageProbeAdvance:
		return handleProbeAdvance(ctx, arg)
	case stageRecordProbe:
		return handleRecordProbeQuantity(ctx, arg, param.Cell)
	case stageScrollProbeDone:
		return handleScrollProbeDone(ctx, arg)
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
	param := actionParam{
		VoucherConfigPath:   defaultVoucherConfigPath,
		WarehouseScanPath:   defaultWarehouseScanPath,
		ReservedOriginium:   29,
		OriginiumToOroberyl: 75,
		OroberylPerPull:     500,
		NextPoolShopPulls:   5,
		NextPoolSigninPulls: 5,
	}

	if strings.TrimSpace(raw) != "" {
		if err := json.Unmarshal([]byte(raw), &param); err != nil {
			return nil, err
		}
	}

	param.Stage = strings.TrimSpace(param.Stage)
	param.VoucherConfigPath = strings.TrimSpace(param.VoucherConfigPath)
	if param.VoucherConfigPath == "" {
		param.VoucherConfigPath = defaultVoucherConfigPath
	}
	param.WarehouseScanPath = strings.TrimSpace(param.WarehouseScanPath)
	if param.WarehouseScanPath == "" {
		param.WarehouseScanPath = defaultWarehouseScanPath
	}
	if param.OriginiumToOroberyl <= 0 {
		return nil, fmt.Errorf("originium_to_oroberyl must be positive")
	}
	if param.OroberylPerPull <= 0 {
		return nil, fmt.Errorf("oroberyl_per_pull must be positive")
	}
	if param.NextPoolShopPulls < 0 {
		return nil, fmt.Errorf("next_pool_shop_pulls must be non-negative")
	}
	if param.NextPoolSigninPulls < 0 {
		return nil, fmt.Errorf("next_pool_signin_pulls must be non-negative")
	}
	return &param, nil
}

// resolveStage keeps the old main-node entry compatible with an empty stage parameter.
func resolveStage(stage string, currentTaskName string) string {
	if strings.TrimSpace(stage) != "" {
		return strings.TrimSpace(stage)
	}
	if currentTaskName == "PullCountCalculatorMain" {
		return stageInit
	}
	return ""
}
