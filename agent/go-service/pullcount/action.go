package pullcount

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/maafocus"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const (
	componentName = "PullCountCalculator"

	defaultVoucherConfigPath = "data/PullCountCalculator/vouchers.json"

	stageInit            = "init"
	stageRecordOriginium = "record_originium"
	stageRecordOroberyl  = "record_oroberyl"
	stageRecordQuantity  = "record_quantity"
	stageRecordItem      = "record_item"
	stagePageBegin       = "page_begin"
	stagePageDone        = "page_done"
	stageProbeBegin      = "probe_begin"
	stageRecordProbe     = "record_probe_quantity"
	stageScrollProbeDone = "scroll_probe_done"
	stageFinish          = "finish"

	nextWarehouseScrollNode = "PullCountCalculatorWarehouseScrollDown"
	nextPageBeginNode       = "PullCountCalculatorPageBegin"
	nextFinishNode          = "PullCountCalculatorFinish"

	warehouseProbeCellLimit  = 9
	minScrollProbeComparable = 4
)

var (
	_ maa.CustomActionRunner = &Action{}

	sessionMu      sync.Mutex
	currentSession *runSession
)

// Action calculates current and next-version recruitment pulls from Pipeline-provided OCR results.
type Action struct{}

type actionParam struct {
	Stage string `json:"stage"`
	Cell  int    `json:"cell"`

	VoucherConfigPath string `json:"voucher_config_path"`

	ReservedOriginium   int `json:"reserved_originium"`
	OriginiumToOroberyl int `json:"originium_to_oroberyl"`
	OroberylPerPull     int `json:"oroberyl_per_pull"`
	NextPoolShopPulls   int `json:"next_pool_shop_pulls"`
	NextPoolSigninPulls int `json:"next_pool_signin_pulls"`
}

type runSession struct {
	Param        actionParam
	Config       *voucherConfig
	VoucherIndex map[string]voucherDef
	Values       resourceValues

	HasConvertedOriginium bool
	HasOroberyl           bool

	CurrentPageCells  map[int]scannedCell
	VoucherQuantities map[string]int
	LastHeadProbe     map[int]int
	CurrentProbe      map[int]int

	PageCount int
}

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
	case stagePageDone:
		return handlePageDone(ctx)
	case stageProbeBegin:
		return handleProbeBegin(ctx)
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

// --- Stage Handlers --- //

// handleInit loads voucher configuration and starts a fresh scan session.
func handleInit(ctx *maa.Context, param *actionParam) bool {
	config, err := loadVoucherConfig(param.VoucherConfigPath)
	if err != nil {
		log.Error().Err(err).Str("component", componentName).Str("path", param.VoucherConfigPath).Msg("failed to load voucher config")
		maafocus.Print(ctx, i18n.T("pullcount.error.config_failed", err.Error()))
		return false
	}
	index, err := buildVoucherIndex(config)
	if err != nil {
		log.Error().Err(err).Str("component", componentName).Msg("failed to build voucher index")
		maafocus.Print(ctx, i18n.T("pullcount.error.config_failed", err.Error()))
		return false
	}

	currentSession = &runSession{
		Param:             *param,
		Config:            config,
		VoucherIndex:      index,
		CurrentPageCells:  make(map[int]scannedCell),
		VoucherQuantities: make(map[string]int),
	}
	log.Info().Str("component", componentName).Msg("pull count session initialized")
	return true
}

// handleRecordResource stores one resource counter from the current Pipeline OCR result.
func handleRecordResource(ctx *maa.Context, arg *maa.CustomActionArg, convertedOriginium bool) bool {
	session, ok := requireSession(ctx)
	if !ok {
		return false
	}

	value, err := readIntegerFromRecognition(arg.RecognitionDetail)
	if err != nil {
		label := i18n.T("pullcount.resource.oroberyl")
		if convertedOriginium {
			label = i18n.T("pullcount.resource.originium")
		}
		log.Warn().Err(err).Str("component", componentName).Str("resource", label).Msg("failed to read resource OCR")
		maafocus.Print(ctx, i18n.T("pullcount.error.recognition_failed", fmt.Sprintf("%s: %s", label, err.Error())))
		return false
	}

	if convertedOriginium {
		session.Values.ConvertedOriginiumOroberyl = value
		session.HasConvertedOriginium = true
		log.Info().Str("component", componentName).Int("converted_originium_oroberyl", value).Msg("converted originium recorded")
		maafocus.Print(ctx, i18n.T("pullcount.resource_read_success", i18n.T("pullcount.resource.originium"), value))
		return true
	}

	session.Values.Oroberyl = value
	session.HasOroberyl = true
	log.Info().Str("component", componentName).Int("oroberyl", value).Msg("oroberyl recorded")
	maafocus.Print(ctx, i18n.T("pullcount.resource_read_success", i18n.T("pullcount.resource.oroberyl"), value))
	return true
}

// handleRecordQuantity stores the stack count recognized for the current warehouse cell.
func handleRecordQuantity(ctx *maa.Context, arg *maa.CustomActionArg, cell int) bool {
	session, ok := requireSession(ctx)
	if !ok {
		return false
	}
	if cell <= 0 {
		log.Error().Str("component", componentName).Int("cell", cell).Msg("invalid cell for quantity stage")
		maafocus.Print(ctx, i18n.T("pullcount.error.invalid_params"))
		return false
	}

	quantity, err := readIntegerFromRecognition(arg.RecognitionDetail)
	if err != nil || quantity <= 0 {
		log.Debug().Err(err).Str("component", componentName).Int("cell", cell).Msg("quantity OCR ignored")
		return true
	}

	recordPageQuantity(session, cell, quantity)
	log.Debug().Str("component", componentName).Int("cell", cell).Int("quantity", quantity).Msg("warehouse cell quantity recorded")
	return true
}

// handleRecordItem stores the selected item title with its cell-indexed quantity.
func handleRecordItem(ctx *maa.Context, arg *maa.CustomActionArg, cell int) bool {
	session, ok := requireSession(ctx)
	if !ok {
		return false
	}
	if cell <= 0 {
		log.Error().Str("component", componentName).Int("cell", cell).Msg("invalid cell for item stage")
		maafocus.Print(ctx, i18n.T("pullcount.error.invalid_params"))
		return false
	}

	title, ok := readTitleFromRecognition(arg.RecognitionDetail)
	if !ok {
		log.Debug().Str("component", componentName).Int("cell", cell).Msg("warehouse item title OCR empty")
		return true
	}

	recordPageItem(session, cell, title)
	quantity := session.CurrentPageCells[cell].Quantity
	if quantity <= 0 {
		quantity = 1
	}
	log.Debug().Str("component", componentName).Int("cell", cell).Str("title", title).Int("quantity", quantity).Msg("warehouse item recorded")
	return true
}

// handlePageBegin clears transient state before scanning a visible warehouse page.
func handlePageBegin(ctx *maa.Context) bool {
	session, ok := requireSession(ctx)
	if !ok {
		return false
	}

	session.CurrentPageCells = make(map[int]scannedCell)
	session.CurrentProbe = nil
	log.Debug().Str("component", componentName).Int("page", session.PageCount+1).Msg("warehouse page scan begin")
	return true
}

// handlePageDone records the scanned page; Pipeline decides whether to scroll or finish.
func handlePageDone(ctx *maa.Context) bool {
	session, ok := requireSession(ctx)
	if !ok {
		return false
	}

	items := recordVisiblePage(session)

	log.Info().
		Str("component", componentName).
		Int("page_count", session.PageCount).
		Int("items", items).
		Str("next", nextWarehouseScrollNode).
		Msg("warehouse page scan done")
	return true
}

// handleProbeBegin clears the lightweight post-scroll quantity probe state.
func handleProbeBegin(ctx *maa.Context) bool {
	session, ok := requireSession(ctx)
	if !ok {
		return false
	}

	session.CurrentProbe = make(map[int]int)
	log.Debug().Str("component", componentName).Int("page", session.PageCount+1).Msg("warehouse scroll probe begin")
	return true
}

// handleRecordProbeQuantity stores one post-scroll quantity OCR result.
func handleRecordProbeQuantity(ctx *maa.Context, arg *maa.CustomActionArg, cell int) bool {
	session, ok := requireSession(ctx)
	if !ok {
		return false
	}
	if cell <= 0 {
		log.Error().Str("component", componentName).Int("cell", cell).Msg("invalid cell for probe quantity stage")
		maafocus.Print(ctx, i18n.T("pullcount.error.invalid_params"))
		return false
	}

	quantity, err := readIntegerFromRecognition(arg.RecognitionDetail)
	if err != nil || quantity <= 0 {
		log.Debug().Err(err).Str("component", componentName).Int("cell", cell).Msg("warehouse probe quantity OCR ignored")
		return true
	}
	if session.CurrentProbe == nil {
		session.CurrentProbe = make(map[int]int)
	}
	session.CurrentProbe[cell] = quantity
	log.Debug().Str("component", componentName).Int("cell", cell).Int("quantity", quantity).Msg("warehouse probe quantity recorded")
	return true
}

// handleScrollProbeDone chooses whether the unchanged post-scroll view should finish or scan.
func handleScrollProbeDone(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	session, ok := requireSession(ctx)
	if !ok {
		return false
	}

	unchanged, comparable, matches := scrollProbeUnchanged(session.LastHeadProbe, session.CurrentProbe)
	nextNode := nextPageBeginNode
	reason := "scroll probe changed"
	if unchanged {
		nextNode = nextFinishNode
		reason = "warehouse scan reached bottom / unchanged after scroll probe"
	}

	if err := ctx.OverrideNext(arg.CurrentTaskName, []maa.NextItem{{Name: nextNode}}); err != nil {
		log.Error().Err(err).Str("component", componentName).Str("next", nextNode).Msg("failed to override scroll probe next")
		maafocus.Print(ctx, i18n.T("pullcount.error.warehouse_scan_failed", err.Error()))
		return false
	}

	log.Info().
		Str("component", componentName).
		Int("comparable", comparable).
		Int("matches", matches).
		Bool("unchanged", unchanged).
		Str("next", nextNode).
		Msg(reason)
	return true
}

// handleFinish summarizes the session and prints the user-visible pull count result.
func handleFinish(ctx *maa.Context) bool {
	session, ok := requireSession(ctx)
	if !ok {
		return false
	}
	defer func() {
		currentSession = nil
	}()

	if !session.HasConvertedOriginium || !session.HasOroberyl {
		err := fmt.Errorf("resource OCR values are incomplete")
		log.Warn().Err(err).Str("component", componentName).Msg("cannot finish pull count")
		maafocus.Print(ctx, i18n.T("pullcount.error.recognition_failed", err.Error()))
		return false
	}

	summary, err := summarizeVouchers(scannedVouchersFromSession(session), session.Config)
	if err != nil {
		log.Error().Err(err).Str("component", componentName).Msg("failed to summarize voucher config")
		maafocus.Print(ctx, i18n.T("pullcount.error.config_failed", err.Error()))
		return false
	}

	result := calculatePullCount(session.Values, summary, &session.Param)
	maafocus.Print(ctx, formatResultFocus(session.Values, result))
	logCalculation(session, summary, result)
	return true
}

// requireSession returns the active run session or reports a user-facing error.
func requireSession(ctx *maa.Context) (*runSession, bool) {
	if currentSession != nil {
		return currentSession, true
	}
	err := fmt.Errorf("pull count session is not initialized")
	log.Error().Err(err).Str("component", componentName).Msg("missing session")
	maafocus.Print(ctx, i18n.T("pullcount.error.invalid_params"))
	return nil, false
}
