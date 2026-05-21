package pullcount

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"unicode"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/maafocus"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/resource"
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

type resourceValues struct {
	ConvertedOriginiumOroberyl int
	Oroberyl                   int
}

type calculationResult struct {
	ReservedOriginium         int
	ReservedOriginiumOroberyl int
	UsableOriginiumOroberyl   int
	ResourcePulls             int
	CurrentOnlyPulls          int
	CarryToNextPulls          int
	NextOnlyPulls             int
	NextPoolShopPulls         int
	NextPoolSigninPulls       int
	CurrentPoolTotal          int
	NextPoolTotal             int
}

type voucherConfig struct {
	Vouchers []voucherDef `json:"vouchers"`
}

type voucherDef struct {
	Name      string   `json:"name"`
	Names     []string `json:"names"`
	PullValue int      `json:"pull_value"`
	PoolScope string   `json:"pool_scope"`
}

type voucherMatch struct {
	CanonicalName string
	DisplayName   string
	PullValue     int
	PoolScope     string
	Quantity      int
	Pulls         int
}

type voucherSummary struct {
	CurrentOnlyPulls int
	CarryToNextPulls int
	NextOnlyPulls    int
	Matches          []voucherMatch
	UnknownNames     []string
}

type scannedVoucher struct {
	Name     string
	Quantity int
}

type scannedCell struct {
	Name        string
	Quantity    int
	HasQuantity bool
}

type pageItem struct {
	Cell        int
	Name        string
	Quantity    int
	HasQuantity bool
}

type runSession struct {
	Param        actionParam
	Config       *voucherConfig
	VoucherIndex map[string]voucherDef
	Values       resourceValues

	HasConvertedOriginium bool
	HasOroberyl           bool

	CurrentPageItems  []scannedVoucher
	CurrentPageCells  map[int]scannedCell
	VoucherQuantities map[string]int
	LastHeadProbe     map[int]int
	CurrentProbe      map[int]int

	PageCount int

	PendingQuantityCell int
	PendingQuantity     int
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
	param.VoucherConfigPath = defaultIfEmpty(param.VoucherConfigPath, defaultVoucherConfigPath)
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

// defaultIfEmpty returns fallback when value is empty after trimming spaces.
func defaultIfEmpty(value string, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
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
	session.PendingQuantityCell = cell
	session.PendingQuantity = quantity
	log.Debug().Str("component", componentName).Int("cell", cell).Int("quantity", quantity).Msg("warehouse cell quantity recorded")
	return true
}

// handleRecordItem stores the selected item title and its pending quantity.
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

	quantity := 1
	hasQuantity := session.PendingQuantityCell == cell && session.PendingQuantity > 0
	if session.PendingQuantityCell == cell && session.PendingQuantity > 0 {
		quantity = session.PendingQuantity
	}
	session.PendingQuantityCell = 0
	session.PendingQuantity = 0

	item := scannedVoucher{Name: title, Quantity: quantity}
	session.CurrentPageItems = append(session.CurrentPageItems, item)
	recordPageItem(session, cell, title, quantity, hasQuantity)
	log.Debug().Str("component", componentName).Int("cell", cell).Str("title", title).Int("quantity", quantity).Msg("warehouse item recorded")
	return true
}

// handlePageBegin clears transient state before scanning a visible warehouse page.
func handlePageBegin(ctx *maa.Context) bool {
	session, ok := requireSession(ctx)
	if !ok {
		return false
	}

	session.CurrentPageItems = nil
	session.CurrentPageCells = make(map[int]scannedCell)
	session.CurrentProbe = nil
	session.PendingQuantityCell = 0
	session.PendingQuantity = 0
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
	maafocus.Print(ctx, formatResultFocus(session.Values, summary, result))
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

// recordPageQuantity stores a visible cell quantity without relying on title OCR order.
func recordPageQuantity(session *runSession, cell int, quantity int) {
	if session.CurrentPageCells == nil {
		session.CurrentPageCells = make(map[int]scannedCell)
	}
	current := session.CurrentPageCells[cell]
	current.Quantity = quantity
	current.HasQuantity = true
	session.CurrentPageCells[cell] = current
}

// recordPageItem stores a visible cell title by cell index so repeated OCR cannot duplicate it.
func recordPageItem(session *runSession, cell int, title string, quantity int, hasQuantity bool) {
	if ignoredPageTitle(title) {
		return
	}
	if session.CurrentPageCells == nil {
		session.CurrentPageCells = make(map[int]scannedCell)
	}
	current := session.CurrentPageCells[cell]
	current.Name = title
	if hasQuantity || !current.HasQuantity {
		current.Quantity = quantity
		current.HasQuantity = hasQuantity
	}
	session.CurrentPageCells[cell] = current
}

// recordVisiblePage accumulates recognized vouchers and stores the top-row probe for the next scroll.
func recordVisiblePage(session *runSession) int {
	items := currentPageItems(session)
	session.LastHeadProbe = headQuantityProbeFromCells(session.CurrentPageCells, warehouseProbeCellLimit)
	accumulatePageVouchers(session, items)
	session.PageCount++
	return len(items)
}

// accumulatePageVouchers keeps the largest visible quantity for each recognized voucher title.
func accumulatePageVouchers(session *runSession, items []pageItem) {
	for _, item := range items {
		if !voucherTitleRelevant(item.Name, session.VoucherIndex) {
			continue
		}
		if item.Quantity > session.VoucherQuantities[item.Name] {
			session.VoucherQuantities[item.Name] = item.Quantity
		}
	}
}

// scannedVouchersFromSession returns a stable list of voucher titles accumulated during scanning.
func scannedVouchersFromSession(session *runSession) []scannedVoucher {
	result := make([]scannedVoucher, 0, len(session.VoucherQuantities))
	for name, quantity := range session.VoucherQuantities {
		result = append(result, scannedVoucher{Name: name, Quantity: quantity})
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})
	return result
}

// currentPageItems returns the best cell-indexed view of the visible warehouse page.
func currentPageItems(session *runSession) []pageItem {
	if len(session.CurrentPageCells) == 0 {
		items := make([]pageItem, 0, len(session.CurrentPageItems))
		for i, item := range session.CurrentPageItems {
			if ignoredPageTitle(item.Name) {
				continue
			}
			items = append(items, pageItem{
				Cell:        i + 1,
				Name:        item.Name,
				Quantity:    item.Quantity,
				HasQuantity: item.Quantity > 1,
			})
		}
		return items
	}

	cells := make([]int, 0, len(session.CurrentPageCells))
	for cell := range session.CurrentPageCells {
		cells = append(cells, cell)
	}
	sort.Ints(cells)

	items := make([]pageItem, 0, len(cells))
	for _, cell := range cells {
		item := session.CurrentPageCells[cell]
		if item.Name == "" || ignoredPageTitle(item.Name) {
			continue
		}
		quantity := item.Quantity
		if quantity <= 0 {
			quantity = 1
		}
		items = append(items, pageItem{
			Cell:        cell,
			Name:        item.Name,
			Quantity:    quantity,
			HasQuantity: item.HasQuantity,
		})
	}
	return items
}

// logCalculation writes structured details for troubleshooting pull-count results.
func logCalculation(session *runSession, summary voucherSummary, result calculationResult) {
	log.Info().
		Str("component", componentName).
		Int("oroberyl", session.Values.Oroberyl).
		Int("reserved_originium", result.ReservedOriginium).
		Int("converted_originium_oroberyl", session.Values.ConvertedOriginiumOroberyl).
		Int("reserved_originium_oroberyl", result.ReservedOriginiumOroberyl).
		Int("usable_converted_originium_oroberyl", result.UsableOriginiumOroberyl).
		Int("resource_pulls", result.ResourcePulls).
		Int("current_only_pulls", result.CurrentOnlyPulls).
		Int("carry_to_next_pulls", result.CarryToNextPulls).
		Int("next_only_pulls", result.NextOnlyPulls).
		Int("next_pool_shop_pulls", result.NextPoolShopPulls).
		Int("next_pool_signin_pulls", result.NextPoolSigninPulls).
		Int("current_pool_total", result.CurrentPoolTotal).
		Int("next_pool_total", result.NextPoolTotal).
		Interface("voucher_matches", summary.Matches).
		Strs("unknown_vouchers", summary.UnknownNames).
		Msg("pull count calculated")
}

// --- OCR Detail Reading --- //

// readIntegerFromRecognition extracts the first integer-like OCR value from Pipeline recognition detail.
func readIntegerFromRecognition(detail *maa.RecognitionDetail) (int, error) {
	if detail == nil || !detail.Hit {
		return 0, fmt.Errorf("OCR not hit")
	}
	for _, text := range ocrTextCandidates(detail) {
		value, err := parseIntegerText(text)
		if err == nil {
			return value, nil
		}
	}
	return 0, fmt.Errorf("no integer OCR result")
}

// readTitleFromRecognition extracts and cleans the first non-empty item title from Pipeline OCR.
func readTitleFromRecognition(detail *maa.RecognitionDetail) (string, bool) {
	if detail == nil || !detail.Hit {
		return "", false
	}
	for _, text := range ocrTextCandidates(detail) {
		if cleaned := cleanTitleText(text); cleaned != "" {
			return cleaned, true
		}
	}
	return "", false
}

// ocrTextCandidates returns OCR texts in preferred reading order.
func ocrTextCandidates(detail *maa.RecognitionDetail) []string {
	texts := make([]string, 0)
	seen := make(map[string]struct{})
	appendText := func(text string) {
		text = strings.TrimSpace(text)
		if text == "" {
			return
		}
		if _, exists := seen[text]; exists {
			return
		}
		seen[text] = struct{}{}
		texts = append(texts, text)
	}

	appendOCRResults(detail, appendText)
	for _, text := range detailJSONOCRTexts(detail.DetailJson) {
		appendText(text)
	}
	for _, child := range detail.CombinedResult {
		for _, text := range ocrTextCandidates(child) {
			appendText(text)
		}
	}
	return texts
}

// appendOCRResults appends OCR text from MaaFramework parsed recognition results.
func appendOCRResults(detail *maa.RecognitionDetail, appendText func(string)) {
	if detail == nil || detail.Results == nil {
		return
	}
	sources := [][]*maa.RecognitionResult{
		resultsFromBest(detail.Results.Best),
		detail.Results.Filtered,
		detail.Results.All,
	}
	for _, source := range sources {
		for _, result := range source {
			if result == nil {
				continue
			}
			ocrResult, ok := result.AsOCR()
			if !ok {
				continue
			}
			appendText(ocrResult.Text)
		}
	}
}

// detailJSONOCRTexts parses OCR text from raw detail JSON when tests or diagnostics provide it directly.
func detailJSONOCRTexts(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}

	var payload struct {
		Text string `json:"text"`
		Best *struct {
			Text string `json:"text"`
		} `json:"best"`
		Filtered []struct {
			Text string `json:"text"`
		} `json:"filtered"`
		All []struct {
			Text string `json:"text"`
		} `json:"all"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return nil
	}

	texts := make([]string, 0)
	if payload.Text != "" {
		texts = append(texts, payload.Text)
	}
	if payload.Best != nil {
		texts = append(texts, payload.Best.Text)
	}
	for _, item := range payload.Filtered {
		texts = append(texts, item.Text)
	}
	for _, item := range payload.All {
		texts = append(texts, item.Text)
	}
	return texts
}

// resultsFromBest wraps a best result so it can share list processing code.
func resultsFromBest(best *maa.RecognitionResult) []*maa.RecognitionResult {
	if best == nil {
		return nil
	}
	return []*maa.RecognitionResult{best}
}

// findRecognitionDetailByName returns the named detail from a nested recognition tree.
func findRecognitionDetailByName(detail *maa.RecognitionDetail, targetName string) *maa.RecognitionDetail {
	if detail == nil {
		return nil
	}
	if detail.Name == targetName {
		return detail
	}
	for _, child := range detail.CombinedResult {
		if found := findRecognitionDetailByName(child, targetName); found != nil {
			return found
		}
	}
	return nil
}

// parseIntegerText extracts the first decimal counter from OCR text.
func parseIntegerText(text string) (int, error) {
	var b strings.Builder
	started := false
	for _, r := range text {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
			started = true
			continue
		}
		if started && isNumberSeparator(r) {
			continue
		}
		if started {
			break
		}
	}
	digits := b.String()
	if digits == "" {
		return 0, fmt.Errorf("no digits in %q", text)
	}
	return strconv.Atoi(digits)
}

// isNumberSeparator reports whether a rune is a thousands separator inside OCR text.
func isNumberSeparator(r rune) bool {
	return r == ','
}

// cleanTitleText removes common OCR decorations while keeping the item name.
func cleanTitleText(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	fields := strings.Fields(text)
	for _, field := range fields {
		if normalizeName(field) != "" {
			return strings.Trim(field, "[]|")
		}
	}
	return strings.Trim(text, "[]|")
}

// ignoredPageTitle reports UI labels that are not warehouse item titles.
func ignoredPageTitle(text string) bool {
	switch normalizeName(text) {
	case "", normalizeName("珍贵物品"), normalizeName("貴重品"), normalizeName("Precious Items"):
		return true
	default:
		return false
	}
}

// headQuantityProbeFromCells returns top quantity OCR results, including cells whose title OCR missed.
func headQuantityProbeFromCells(cells map[int]scannedCell, limit int) map[int]int {
	if limit <= 0 {
		return nil
	}
	probe := make(map[int]int)
	for cell, item := range cells {
		if cell <= 0 || cell > limit || !item.HasQuantity || item.Quantity <= 0 {
			continue
		}
		probe[cell] = item.Quantity
	}
	return probe
}

// scrollProbeUnchanged compares pre-scroll and post-scroll top quantity vectors.
func scrollProbeUnchanged(before map[int]int, after map[int]int) (bool, int, int) {
	comparable := 0
	matches := 0
	for cell, beforeValue := range before {
		afterValue, ok := after[cell]
		if !ok {
			continue
		}
		comparable++
		if beforeValue == afterValue {
			matches++
		}
	}
	return comparable >= minScrollProbeComparable && matches == comparable, comparable, matches
}

// --- Voucher Config And Classification --- //

// loadVoucherConfig reads voucher definitions from assets data.
func loadVoucherConfig(path string) (*voucherConfig, error) {
	var config voucherConfig
	if err := resource.ReadJsonResource(path, &config); err != nil {
		return nil, err
	}
	for i := range config.Vouchers {
		if err := validateVoucherDef(config.Vouchers[i]); err != nil {
			return nil, fmt.Errorf("voucher %d: %w", i, err)
		}
	}
	if _, err := buildVoucherIndex(&config); err != nil {
		return nil, err
	}
	return &config, nil
}

// validateVoucherDef checks a voucher definition before it is used for totals.
func validateVoucherDef(def voucherDef) error {
	if strings.TrimSpace(def.Name) == "" && len(def.Names) == 0 {
		return fmt.Errorf("name or names is required")
	}
	if def.PullValue != 1 && def.PullValue != 10 {
		return fmt.Errorf("pull_value must be 1 or 10")
	}
	switch def.PoolScope {
	case "current_only", "carry_to_next", "next_only", "ignore":
		return nil
	default:
		return fmt.Errorf("pool_scope must be current_only, carry_to_next, next_only, or ignore")
	}
}

// summarizeVouchers classifies scanned items and totals pull values by pool scope.
func summarizeVouchers(scanned []scannedVoucher, config *voucherConfig) (voucherSummary, error) {
	index, err := buildVoucherIndex(config)
	if err != nil {
		return voucherSummary{}, err
	}
	summary := voucherSummary{}
	unknown := make(map[string]struct{})

	for _, item := range scanned {
		if item.Name == "" || item.Quantity <= 0 {
			continue
		}
		def, ok := index[normalizeName(item.Name)]
		if !ok {
			if mayBeRecruitmentVoucherName(item.Name) {
				unknown[item.Name] = struct{}{}
			}
			continue
		}
		pulls := item.Quantity * def.PullValue
		if def.PoolScope == "ignore" {
			pulls = 0
		}
		match := voucherMatch{
			CanonicalName: def.Name,
			DisplayName:   item.Name,
			PullValue:     def.PullValue,
			PoolScope:     def.PoolScope,
			Quantity:      item.Quantity,
			Pulls:         pulls,
		}
		summary.Matches = append(summary.Matches, match)
		switch def.PoolScope {
		case "current_only":
			summary.CurrentOnlyPulls += pulls
		case "carry_to_next":
			summary.CarryToNextPulls += pulls
		case "next_only":
			summary.NextOnlyPulls += pulls
		}
	}

	for name := range unknown {
		summary.UnknownNames = append(summary.UnknownNames, name)
	}
	sort.Strings(summary.UnknownNames)
	sort.Slice(summary.Matches, func(i, j int) bool {
		if summary.Matches[i].PoolScope != summary.Matches[j].PoolScope {
			return summary.Matches[i].PoolScope < summary.Matches[j].PoolScope
		}
		return summary.Matches[i].DisplayName < summary.Matches[j].DisplayName
	})
	return summary, nil
}

// buildVoucherIndex maps every configured alias to its voucher definition.
func buildVoucherIndex(config *voucherConfig) (map[string]voucherDef, error) {
	index := make(map[string]voucherDef)
	if config == nil {
		return index, nil
	}
	owners := make(map[string]int)
	for i, def := range config.Vouchers {
		aliases := append([]string{def.Name}, def.Names...)
		for _, alias := range aliases {
			key := normalizeName(alias)
			if key == "" {
				continue
			}
			if owner, exists := owners[key]; exists {
				if owner == i {
					continue
				}
				return nil, fmt.Errorf(
					"duplicate voucher alias %q normalized as %q for voucher %d (%s) and voucher %d (%s)",
					alias,
					key,
					owner,
					voucherDefDisplayName(config.Vouchers[owner]),
					i,
					voucherDefDisplayName(def),
				)
			}
			owners[key] = i
			index[key] = def
		}
	}
	return index, nil
}

// voucherDefDisplayName returns a stable label for config validation errors.
func voucherDefDisplayName(def voucherDef) string {
	if strings.TrimSpace(def.Name) != "" {
		return def.Name
	}
	for _, name := range def.Names {
		if strings.TrimSpace(name) != "" {
			return name
		}
	}
	return "<unnamed>"
}

// normalizeName strips OCR noise for exact item-name matching.
func normalizeName(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch r {
		case '[', ']', '|', '(', ')', '-', '_', '.', ',', '、', '·', '/', '\\', '：', ':', '；', ';', '。':
			continue
		}
		if unicode.IsSpace(r) {
			continue
		}
		b.WriteRune(unicode.ToLower(r))
	}
	return b.String()
}

// mayBeRecruitmentVoucherName reports whether an unconfigured item looks relevant to pull counting.
func mayBeRecruitmentVoucherName(name string) bool {
	normalized := normalizeName(name)
	if normalized == "" {
		return false
	}
	keywords := []string{
		"寻访",
		"尋訪",
		"憑證",
		"凭证",
		"スカウト",
		"募集",
		"모집",
		"recruit",
		"voucher",
		"ticket",
	}
	for _, keyword := range keywords {
		if strings.Contains(normalized, normalizeName(keyword)) {
			return true
		}
	}
	return false
}

// voucherTitleRelevant keeps configured vouchers and unconfigured names that look recruitment-related.
func voucherTitleRelevant(name string, configuredNames map[string]voucherDef) bool {
	key := normalizeName(name)
	if key == "" {
		return false
	}
	if _, ok := configuredNames[key]; ok {
		return true
	}
	return mayBeRecruitmentVoucherName(name)
}

// --- Calculation And Display --- //

// calculatePullCount converts resources and classified vouchers into current and next-pool totals.
func calculatePullCount(values resourceValues, summary voucherSummary, param *actionParam) calculationResult {
	reservedOriginiumOroberyl := param.ReservedOriginium * param.OriginiumToOroberyl
	usableOriginiumOroberyl := values.ConvertedOriginiumOroberyl - reservedOriginiumOroberyl
	if usableOriginiumOroberyl < 0 {
		usableOriginiumOroberyl = 0
	}

	resourcePulls := (values.Oroberyl + usableOriginiumOroberyl) / param.OroberylPerPull
	currentPoolTotal := resourcePulls + summary.CurrentOnlyPulls + summary.CarryToNextPulls
	nextPoolTotal := resourcePulls + summary.CarryToNextPulls + summary.NextOnlyPulls + param.NextPoolShopPulls + param.NextPoolSigninPulls

	return calculationResult{
		ReservedOriginium:         param.ReservedOriginium,
		ReservedOriginiumOroberyl: reservedOriginiumOroberyl,
		UsableOriginiumOroberyl:   usableOriginiumOroberyl,
		ResourcePulls:             resourcePulls,
		CurrentOnlyPulls:          summary.CurrentOnlyPulls,
		CarryToNextPulls:          summary.CarryToNextPulls,
		NextOnlyPulls:             summary.NextOnlyPulls,
		NextPoolShopPulls:         param.NextPoolShopPulls,
		NextPoolSigninPulls:       param.NextPoolSigninPulls,
		CurrentPoolTotal:          currentPoolTotal,
		NextPoolTotal:             nextPoolTotal,
	}
}

// formatResultFocus builds the user-visible calculation summary.
func formatResultFocus(values resourceValues, summary voucherSummary, result calculationResult) string {
	message := i18n.T(
		"pullcount.result",
		result.ResourcePulls,
		result.CurrentOnlyPulls,
		result.CarryToNextPulls,
		result.NextOnlyPulls,
		result.NextPoolShopPulls,
		result.NextPoolSigninPulls,
		result.CurrentPoolTotal,
		result.NextPoolTotal,
		values.Oroberyl,
		values.ConvertedOriginiumOroberyl,
		result.ReservedOriginium,
		result.ReservedOriginiumOroberyl,
		result.UsableOriginiumOroberyl,
	)
	if len(summary.UnknownNames) > 0 {
		message += "\n" + i18n.T("pullcount.result.unknown_vouchers", strings.Join(summary.UnknownNames, i18n.Separator()))
	}
	return message
}
