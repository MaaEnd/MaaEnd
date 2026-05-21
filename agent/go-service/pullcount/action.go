package pullcount

import (
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/maafocus"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/resource"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const (
	componentName = "PullCountCalculator"

	defaultOriginiumNode       = "PullCountCalculatorOriginiumOCR"
	defaultOroberylNode        = "PullCountCalculatorOroberylOCR"
	defaultValuablesSceneNode  = "SceneStashValuablesTab"
	defaultWarehouseScrollNode = "PullCountCalculatorWarehouseScrollDown"
	defaultVoucherConfigPath   = "data/PullCountCalculator/vouchers.json"

	fallbackValuablesMenuNode = "__ScenePrivateMenuListEnterMenuValuables"
	fallbackValuablesTabNode  = "_SceneStashValuablesTabIn"
	fallbackValuablesSwitch   = "_SceneStashValuablesTabSwitch"
)

var _ maa.CustomActionRunner = &Action{}

// Action calculates current and next-version recruitment pulls from resources and warehouse vouchers.
type Action struct{}

type actionParam struct {
	OriginiumNode      string `json:"originium_node"`
	OroberylNode       string `json:"oroberyl_node"`
	ValuablesSceneNode string `json:"valuables_scene_node"`
	VoucherConfigPath  string `json:"voucher_config_path"`

	ReservedOriginium   int `json:"reserved_originium"`
	OriginiumToOroberyl int `json:"originium_to_oroberyl"`
	OroberylPerPull     int `json:"oroberyl_per_pull"`
	NextPoolShopPulls   int `json:"next_pool_shop_pulls"`
	NextPoolSigninPulls int `json:"next_pool_signin_pulls"`

	ScanMaxPages   int       `json:"scan_max_pages"`
	ScanClickDelay int       `json:"scan_click_delay_ms"`
	ScanScrollNode string    `json:"scan_scroll_node"`
	ScanGrid       gridParam `json:"scan_grid"`
	TitleROI       roiParam  `json:"title_roi"`
	QuantityROI    roiParam  `json:"quantity_roi"`
	ScrollCheckROI roiParam  `json:"scroll_check_roi"`

	ScrollChangeThreshold float64 `json:"scroll_change_threshold"`
}

type gridParam struct {
	StartX int `json:"start_x"`
	StartY int `json:"start_y"`
	StepX  int `json:"step_x"`
	StepY  int `json:"step_y"`
	Cols   int `json:"cols"`
	Rows   int `json:"rows"`
	MaxY   int `json:"max_y"`
}

type roiParam struct {
	X int `json:"x"`
	Y int `json:"y"`
	W int `json:"w"`
	H int `json:"h"`
}

type resourceValues struct {
	Originium int
	Oroberyl  int
}

type calculationResult struct {
	ReservedOriginium      int
	ReservedOriginiumValue int
	UsableOriginium        int
	ResourcePulls          int
	CurrentOnlyPulls       int
	CarryToNextPulls       int
	NextOnlyPulls          int
	NextPoolShopPulls      int
	NextPoolSigninPulls    int
	CurrentPoolTotal       int
	NextPoolTotal          int
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

type visiblePageScan struct {
	Items    []scannedVoucher
	Vouchers []scannedVoucher
}

type scaledLayout struct {
	ScaleX float64
	ScaleY float64
	Grid   []gridCell
	Title  roiParam
	Qty    roiParam
}

type gridCell struct {
	X int
	Y int
}

// --- Entry And Parameters --- //

// Run reads recruitment resources, scans warehouse vouchers, and prints the pull totals.
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

	img, err := captureCurrentImage(ctx)
	if err != nil {
		log.Error().Err(err).Str("component", componentName).Msg("failed to capture image")
		maafocus.Print(ctx, i18n.T("pullcount.error.capture_failed"))
		return false
	}

	values, err := readResourceValues(ctx, img, param)
	if err != nil {
		log.Warn().Err(err).Str("component", componentName).Msg("failed to read resource values")
		maafocus.Print(ctx, i18n.T("pullcount.error.recognition_failed", err.Error()))
		return false
	}

	config, err := loadVoucherConfig(param.VoucherConfigPath)
	if err != nil {
		log.Error().Err(err).Str("component", componentName).Str("path", param.VoucherConfigPath).Msg("failed to load voucher config")
		maafocus.Print(ctx, i18n.T("pullcount.error.config_failed", err.Error()))
		return false
	}

	if err := enterValuablesTab(ctx, param); err != nil {
		log.Warn().Err(err).Str("component", componentName).Msg("failed to enter valuables tab")
		if fallbackErr := enterValuablesTabFromCurrentMenu(ctx); fallbackErr != nil {
			log.Warn().Err(fallbackErr).Str("component", componentName).Msg("failed to enter valuables tab from current menu fallback")
			maafocus.Print(ctx, i18n.T("pullcount.error.enter_valuables_failed", fmt.Sprintf("%s；fallback: %s", err.Error(), fallbackErr.Error())))
			return false
		}
	}

	scanned, err := scanWarehouseVouchers(ctx, param, config)
	if err != nil {
		log.Warn().Err(err).Str("component", componentName).Msg("failed to scan warehouse vouchers")
		maafocus.Print(ctx, i18n.T("pullcount.error.warehouse_scan_failed", err.Error()))
		return false
	}

	summary := summarizeVouchers(scanned, config)
	result := calculatePullCount(values, summary, param)
	maafocus.Print(ctx, formatResultFocus(values, summary, result))

	log.Info().
		Str("component", componentName).
		Int("oroberyl", values.Oroberyl).
		Int("reserved_originium", result.ReservedOriginium).
		Int("converted_originium_oroberyl", values.Originium).
		Int("reserved_originium_oroberyl", result.ReservedOriginiumValue).
		Int("usable_converted_originium_oroberyl", result.UsableOriginium).
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

	return true
}

// parseActionParam parses the custom action config and fills default constants.
func parseActionParam(raw string) (*actionParam, error) {
	param := actionParam{
		OriginiumNode:       defaultOriginiumNode,
		OroberylNode:        defaultOroberylNode,
		ValuablesSceneNode:  defaultValuablesSceneNode,
		VoucherConfigPath:   defaultVoucherConfigPath,
		ReservedOriginium:   29,
		OriginiumToOroberyl: 75,
		OroberylPerPull:     500,
		NextPoolShopPulls:   5,
		NextPoolSigninPulls: 5,
		ScanMaxPages:        8,
		ScanClickDelay:      180,
		ScanScrollNode:      defaultWarehouseScrollNode,
		ScanGrid: gridParam{
			StartX: 85,
			StartY: 168,
			StepX:  103,
			StepY:  103,
			Cols:   9,
			Rows:   5,
			MaxY:   640,
		},
		TitleROI: roiParam{
			X: 980,
			Y: 82,
			W: 250,
			H: 48,
		},
		QuantityROI: roiParam{
			X: -24,
			Y: 18,
			W: 58,
			H: 32,
		},
		ScrollCheckROI: roiParam{
			X: 40,
			Y: 120,
			W: 930,
			H: 540,
		},
		ScrollChangeThreshold: 0.01,
	}

	if strings.TrimSpace(raw) == "" {
		return &param, nil
	}

	if err := json.Unmarshal([]byte(raw), &param); err != nil {
		return nil, err
	}

	param.OriginiumNode = defaultIfEmpty(param.OriginiumNode, defaultOriginiumNode)
	param.OroberylNode = defaultIfEmpty(param.OroberylNode, defaultOroberylNode)
	param.ValuablesSceneNode = defaultIfEmpty(param.ValuablesSceneNode, defaultValuablesSceneNode)
	param.ScanScrollNode = defaultIfEmpty(param.ScanScrollNode, defaultWarehouseScrollNode)
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
	if param.ScanMaxPages <= 0 {
		return nil, fmt.Errorf("scan_max_pages must be positive")
	}
	if param.ScanClickDelay < 0 {
		return nil, fmt.Errorf("scan_click_delay_ms must be non-negative")
	}
	if err := validateGridParam(param.ScanGrid); err != nil {
		return nil, err
	}
	if err := validateROIParam("title_roi", param.TitleROI); err != nil {
		return nil, err
	}
	if err := validateROIParam("quantity_roi", param.QuantityROI); err != nil {
		return nil, err
	}
	if err := validateROIParam("scroll_check_roi", param.ScrollCheckROI); err != nil {
		return nil, err
	}
	if param.ScrollChangeThreshold < 0 || param.ScrollChangeThreshold > 1 {
		return nil, fmt.Errorf("scroll_change_threshold must be between 0 and 1")
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

// validateGridParam checks the visible warehouse grid shape.
func validateGridParam(grid gridParam) error {
	if grid.Cols <= 0 || grid.Rows <= 0 {
		return fmt.Errorf("scan_grid cols and rows must be positive")
	}
	if grid.StepX <= 0 || grid.StepY <= 0 {
		return fmt.Errorf("scan_grid step_x and step_y must be positive")
	}
	if grid.MaxY <= 0 {
		return fmt.Errorf("scan_grid max_y must be positive")
	}
	return nil
}

// validateROIParam checks that an OCR ROI has a usable size.
func validateROIParam(name string, roi roiParam) error {
	if roi.W <= 0 || roi.H <= 0 {
		return fmt.Errorf("%s width and height must be positive", name)
	}
	return nil
}

// --- OCR Reading --- //

// captureCurrentImage requests a fresh screenshot from the current controller.
func captureCurrentImage(ctx *maa.Context) (image.Image, error) {
	tasker := ctx.GetTasker()
	if tasker == nil {
		return nil, fmt.Errorf("tasker is nil")
	}

	controller := tasker.GetController()
	if controller == nil {
		return nil, fmt.Errorf("controller is nil")
	}

	controller.PostScreencap().Wait()
	img, err := controller.CacheImage()
	if err != nil {
		return nil, err
	}
	if img == nil {
		return nil, fmt.Errorf("cached image is nil")
	}
	return img, nil
}

// readResourceValues reads resource counters from the recruitment top bar.
func readResourceValues(ctx *maa.Context, img image.Image, param *actionParam) (resourceValues, error) {
	var values resourceValues
	var err error

	if values.Originium, err = runIntegerOCR(ctx, img, param.OriginiumNode, i18n.T("pullcount.resource.originium")); err != nil {
		return values, err
	}
	if values.Oroberyl, err = runIntegerOCR(ctx, img, param.OroberylNode, i18n.T("pullcount.resource.oroberyl")); err != nil {
		return values, err
	}

	return values, nil
}

// runIntegerOCR executes one OCR node and extracts the first integer-like value.
func runIntegerOCR(ctx *maa.Context, img image.Image, nodeName string, label string) (int, error) {
	detail, err := ctx.RunRecognition(nodeName, img)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", label, err)
	}
	if detail == nil || !detail.Hit {
		return 0, fmt.Errorf("%s: OCR not hit", label)
	}

	for _, text := range ocrTextCandidates(detail) {
		value, err := parseIntegerText(text)
		if err == nil {
			return value, nil
		}
	}

	return 0, fmt.Errorf("%s: no integer OCR result", label)
}

// ocrTextCandidates returns OCR texts in preferred reading order.
func ocrTextCandidates(detail *maa.RecognitionDetail) []string {
	if detail == nil || detail.Results == nil {
		return nil
	}

	sources := [][]*maa.RecognitionResult{
		resultsFromBest(detail.Results.Best),
		detail.Results.Filtered,
		detail.Results.All,
	}

	texts := make([]string, 0)
	seen := make(map[string]struct{})
	for _, source := range sources {
		for _, result := range source {
			if result == nil {
				continue
			}
			ocrResult, ok := result.AsOCR()
			if !ok {
				continue
			}
			text := strings.TrimSpace(ocrResult.Text)
			if text == "" {
				continue
			}
			if _, exists := seen[text]; exists {
				continue
			}
			seen[text] = struct{}{}
			texts = append(texts, text)
		}
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

// --- Warehouse Scanning --- //

// enterValuablesTab uses the existing scene manager route to open the valuables tab.
func enterValuablesTab(ctx *maa.Context, param *actionParam) error {
	detail, err := ctx.RunTask(param.ValuablesSceneNode)
	if err != nil {
		return err
	}
	if detail == nil {
		return fmt.Errorf("%s returned nil detail", param.ValuablesSceneNode)
	}
	if !detail.Status.Success() {
		return fmt.Errorf("%s status is %s", param.ValuablesSceneNode, detail.Status.String())
	}
	return nil
}

// enterValuablesTabFromCurrentMenu handles the common case where the menu list is already open.
func enterValuablesTabFromCurrentMenu(ctx *maa.Context) error {
	if detail, err := ctx.RunTask(fallbackValuablesMenuNode); err == nil && detail != nil && detail.Status.Success() {
		return switchValuablesTab(ctx)
	}
	if err := clickValuablesByVisibleText(ctx); err != nil {
		return err
	}
	return switchValuablesTab(ctx)
}

// switchValuablesTab ensures the valuables page is on the Precious Items tab.
func switchValuablesTab(ctx *maa.Context) error {
	if detail, err := ctx.RunTask(fallbackValuablesTabNode); err == nil && detail != nil && detail.Status.Success() {
		return nil
	}
	detail, err := ctx.RunTask(fallbackValuablesSwitch)
	if err != nil {
		return err
	}
	if detail == nil {
		return fmt.Errorf("%s returned nil detail", fallbackValuablesSwitch)
	}
	if !detail.Status.Success() {
		return fmt.Errorf("%s status is %s", fallbackValuablesSwitch, detail.Status.String())
	}
	return nil
}

// clickValuablesByVisibleText OCRs the right menu area and clicks the Valuables entry.
func clickValuablesByVisibleText(ctx *maa.Context) error {
	img, err := captureCurrentImage(ctx)
	if err != nil {
		return err
	}
	detail, err := ctx.RunRecognitionDirect(maa.RecognitionTypeOCR, maa.OCRParam{
		ROI: maa.NewTargetRect(maa.Rect{880, 350, 380, 230}),
		Expected: []string{
			"贵重品库",
			"貴重品庫",
			"貴重品倉庫",
			"Valuables",
			"VALUABLES",
			"STASH",
			"귀중품 창고",
		},
	}, img)
	if err != nil {
		return err
	}
	if detail == nil || !detail.Hit || detail.Results == nil {
		return fmt.Errorf("visible valuables menu OCR not hit")
	}
	box, ok := bestOCRBox(detail)
	if !ok {
		return fmt.Errorf("visible valuables menu OCR has no box")
	}
	ctrl := ctx.GetTasker().GetController()
	ctrl.PostClick(int32(box.X()+box.Width()/2), int32(box.Y()+box.Height()/2)).Wait()
	time.Sleep(1500 * time.Millisecond)
	return nil
}

// bestOCRBox returns the box of the preferred OCR result.
func bestOCRBox(detail *maa.RecognitionDetail) (maa.Rect, bool) {
	if detail == nil || detail.Results == nil {
		return maa.Rect{}, false
	}
	for _, result := range resultsFromBest(detail.Results.Best) {
		if ocr, ok := result.AsOCR(); ok {
			return ocr.Box, true
		}
	}
	for _, group := range [][]*maa.RecognitionResult{detail.Results.Filtered, detail.Results.All} {
		for _, result := range group {
			if result == nil {
				continue
			}
			if ocr, ok := result.AsOCR(); ok {
				return ocr.Box, true
			}
		}
	}
	return maa.Rect{}, false
}

// scanWarehouseVouchers scans visible valuables pages and returns the highest seen quantity per voucher name.
func scanWarehouseVouchers(ctx *maa.Context, param *actionParam, config *voucherConfig) ([]scannedVoucher, error) {
	tasker := ctx.GetTasker()
	if tasker == nil {
		return nil, fmt.Errorf("tasker is nil")
	}
	ctrl := tasker.GetController()
	if ctrl == nil {
		return nil, fmt.Errorf("controller is nil")
	}

	byName := make(map[string]int)
	pageSignatures := make(map[string]struct{})
	configuredNames := buildVoucherIndex(config)

	for page := 0; page < param.ScanMaxPages; page++ {
		if tasker.Stopping() {
			return nil, fmt.Errorf("task is stopping")
		}

		img, err := captureCurrentImage(ctx)
		if err != nil {
			return nil, err
		}
		layout := buildScaledLayout(img, param)
		pageScan := scanVisibleVoucherPage(ctx, ctrl, img, layout, param, configuredNames)
		signature := scannedPageSignature(pageScan.Items)
		if signature != "" {
			if _, seen := pageSignatures[signature]; seen && page > 0 {
				log.Info().Str("component", componentName).Int("page", page).Str("signature", signature).Msg("warehouse scan reached repeated page")
				break
			}
			pageSignatures[signature] = struct{}{}
		}

		for _, item := range pageScan.Vouchers {
			if item.Name == "" {
				continue
			}
			if item.Quantity > byName[item.Name] {
				byName[item.Name] = item.Quantity
			}
		}

		if page+1 >= param.ScanMaxPages {
			break
		}
		beforeScroll, err := captureCurrentImage(ctx)
		if err != nil {
			return nil, err
		}
		if err := scrollWarehousePage(ctx, param); err != nil {
			return nil, err
		}
		afterScroll, err := captureCurrentImage(ctx)
		if err != nil {
			return nil, err
		}
		changed, ratio := warehouseGridChanged(beforeScroll, afterScroll, param.ScrollCheckROI, param.ScrollChangeThreshold)
		if !changed {
			log.Info().
				Str("component", componentName).
				Int("page", page).
				Float64("change_ratio", ratio).
				Float64("threshold", param.ScrollChangeThreshold).
				Msg("warehouse scan reached bottom: unchanged after scroll")
			break
		}
	}

	result := make([]scannedVoucher, 0, len(byName))
	for name, quantity := range byName {
		result = append(result, scannedVoucher{Name: name, Quantity: quantity})
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})
	return result, nil
}

// scanVisibleVoucherPage clicks visible cells, recording all readable items and relevant vouchers.
func scanVisibleVoucherPage(ctx *maa.Context, ctrl *maa.Controller, pageImg image.Image, layout scaledLayout, param *actionParam, configuredNames map[string]voucherDef) visiblePageScan {
	scan := visiblePageScan{
		Items:    make([]scannedVoucher, 0),
		Vouchers: make([]scannedVoucher, 0),
	}
	for _, cell := range layout.Grid {
		qty := readCellQuantity(ctx, pageImg, cell, layout.Qty)

		ctrl.PostClick(int32(cell.X), int32(cell.Y)).Wait()
		if param.ScanClickDelay > 0 {
			time.Sleep(time.Duration(param.ScanClickDelay) * time.Millisecond)
		}

		img, err := captureCurrentImage(ctx)
		if err != nil {
			log.Debug().Err(err).Str("component", componentName).Int("x", cell.X).Int("y", cell.Y).Msg("failed to capture selected cell")
			continue
		}

		title := readVoucherTitle(ctx, img, layout.Title)
		if title == "" {
			continue
		}
		if qty <= 0 {
			qty = 1
		}
		item := scannedVoucher{Name: title, Quantity: qty}
		scan.Items = append(scan.Items, item)
		if !voucherTitleRelevant(title, configuredNames) {
			continue
		}
		scan.Vouchers = append(scan.Vouchers, item)
		log.Debug().Str("component", componentName).Str("title", title).Int("qty", qty).Int("x", cell.X).Int("y", cell.Y).Msg("warehouse voucher cell scanned")
	}
	return scan
}

// buildScaledLayout maps 720p-based coordinates to the current screenshot size.
func buildScaledLayout(img image.Image, param *actionParam) scaledLayout {
	bounds := img.Bounds()
	scaleX := float64(bounds.Dx()) / 1280.0
	scaleY := float64(bounds.Dy()) / 720.0

	grid := make([]gridCell, 0, param.ScanGrid.Cols*param.ScanGrid.Rows)
	for row := 0; row < param.ScanGrid.Rows; row++ {
		y720 := param.ScanGrid.StartY + row*param.ScanGrid.StepY
		if y720 > param.ScanGrid.MaxY {
			continue
		}
		for col := 0; col < param.ScanGrid.Cols; col++ {
			x720 := param.ScanGrid.StartX + col*param.ScanGrid.StepX
			grid = append(grid, gridCell{
				X: scaleCoord(x720, scaleX),
				Y: scaleCoord(y720, scaleY),
			})
		}
	}

	return scaledLayout{
		ScaleX: scaleX,
		ScaleY: scaleY,
		Grid:   grid,
		Title:  scaleROI(param.TitleROI, scaleX, scaleY),
		Qty:    scaleROI(param.QuantityROI, scaleX, scaleY),
	}
}

// scaleCoord scales a 720p coordinate to current screenshot coordinates.
func scaleCoord(value int, scale float64) int {
	return int(math.Round(float64(value) * scale))
}

// scaleROI scales a 720p ROI to current screenshot coordinates.
func scaleROI(roi roiParam, scaleX, scaleY float64) roiParam {
	return roiParam{
		X: scaleCoord(roi.X, scaleX),
		Y: scaleCoord(roi.Y, scaleY),
		W: maxInt(1, scaleCoord(roi.W, scaleX)),
		H: maxInt(1, scaleCoord(roi.H, scaleY)),
	}
}

// offsetROI places a relative ROI around a cell center.
func offsetROI(cell gridCell, roi roiParam) roiParam {
	return roiParam{
		X: cell.X + roi.X,
		Y: cell.Y + roi.Y,
		W: roi.W,
		H: roi.H,
	}
}

// readCellQuantity OCRs the stack count near a grid cell; empty stacks default to one.
func readCellQuantity(ctx *maa.Context, img image.Image, cell gridCell, qtyROI roiParam) int {
	texts := runDirectOCRTexts(ctx, img, offsetROI(cell, qtyROI), []string{".*\\d+.*"}, true)
	for _, text := range texts {
		value, err := parseIntegerText(text)
		if err == nil && value > 0 {
			return value
		}
	}
	return 1
}

// readVoucherTitle OCRs the selected item name in the right detail panel.
func readVoucherTitle(ctx *maa.Context, img image.Image, titleROI roiParam) string {
	texts := runDirectOCRTexts(ctx, img, titleROI, []string{".+"}, true)
	for _, text := range texts {
		if cleaned := cleanTitleText(text); cleaned != "" {
			return cleaned
		}
	}
	return ""
}

// runDirectOCRTexts runs OCR directly on a ROI and returns unique texts.
func runDirectOCRTexts(ctx *maa.Context, img image.Image, roi roiParam, expected []string, onlyRec bool) []string {
	param := maa.OCRParam{
		ROI:      maa.NewTargetRect(maa.Rect{roi.X, roi.Y, roi.W, roi.H}),
		Expected: expected,
		OnlyRec:  onlyRec,
	}
	detail, err := ctx.RunRecognitionDirect(maa.RecognitionTypeOCR, param, img)
	if err != nil || detail == nil || !detail.Hit {
		return nil
	}
	return ocrTextCandidates(detail)
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

// scannedPageSignature builds a stable signature for scroll-end detection.
func scannedPageSignature(items []scannedVoucher) string {
	if len(items) == 0 {
		return ""
	}
	parts := make([]string, 0, len(items))
	for _, item := range items {
		if item.Name == "" {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s:%d", normalizeName(item.Name), item.Quantity))
	}
	if len(parts) == 0 {
		return ""
	}
	sort.Strings(parts)
	return strings.Join(parts, "|")
}

// scrollWarehousePage scrolls the valuables grid down to expose more cells.
func scrollWarehousePage(ctx *maa.Context, param *actionParam) error {
	detail, err := ctx.RunTask(param.ScanScrollNode)
	if err != nil {
		return err
	}
	if detail == nil {
		return fmt.Errorf("%s returned nil detail", param.ScanScrollNode)
	}
	if !detail.Status.Success() {
		return fmt.Errorf("%s status is %s", param.ScanScrollNode, detail.Status.String())
	}
	return nil
}

// warehouseGridChanged reports whether the configured warehouse grid ROI changed after scrolling.
func warehouseGridChanged(before image.Image, after image.Image, roi roiParam, threshold float64) (bool, float64) {
	ratio := changedPixelRatio(before, after, roi)
	if threshold <= 0 {
		return ratio > 0, ratio
	}
	return ratio >= threshold, ratio
}

// changedPixelRatio calculates the proportion of meaningfully changed pixels inside a 720p-based ROI.
func changedPixelRatio(before image.Image, after image.Image, roi roiParam) float64 {
	if before == nil || after == nil {
		return 0
	}

	rect := scaledImageRect(roi, before.Bounds())
	rect = rect.Intersect(after.Bounds())
	if rect.Empty() {
		return 0
	}

	changed := 0
	total := 0
	for y := rect.Min.Y; y < rect.Max.Y; y++ {
		for x := rect.Min.X; x < rect.Max.X; x++ {
			total++
			if colorsMeaningfullyDifferent(before.At(x, y), after.At(x, y)) {
				changed++
			}
		}
	}
	if total == 0 {
		return 0
	}
	return float64(changed) / float64(total)
}

// scaledImageRect maps a 720p ROI to an image rectangle and clamps it to the image bounds.
func scaledImageRect(roi roiParam, bounds image.Rectangle) image.Rectangle {
	scaleX := float64(bounds.Dx()) / 1280.0
	scaleY := float64(bounds.Dy()) / 720.0
	rect := image.Rect(
		bounds.Min.X+scaleCoord(roi.X, scaleX),
		bounds.Min.Y+scaleCoord(roi.Y, scaleY),
		bounds.Min.X+scaleCoord(roi.X+roi.W, scaleX),
		bounds.Min.Y+scaleCoord(roi.Y+roi.H, scaleY),
	)
	return rect.Intersect(bounds)
}

// colorsMeaningfullyDifferent ignores tiny capture noise while detecting real UI movement.
func colorsMeaningfullyDifferent(a color.Color, b color.Color) bool {
	ar, ag, ab, _ := a.RGBA()
	br, bg, bb, _ := b.RGBA()
	const channelNoise = uint32(8 * 257)
	return absDiffUint32(ar, br)+absDiffUint32(ag, bg)+absDiffUint32(ab, bb) > channelNoise*3
}

// absDiffUint32 returns the absolute difference between two color channels.
func absDiffUint32(a uint32, b uint32) uint32 {
	if a > b {
		return a - b
	}
	return b - a
}

// maxInt returns the larger integer.
func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
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
	case "current_only", "carry_to_next", "next_only":
		return nil
	default:
		return fmt.Errorf("pool_scope must be current_only, carry_to_next, or next_only")
	}
}

// summarizeVouchers classifies scanned items and totals pull values by pool scope.
func summarizeVouchers(scanned []scannedVoucher, config *voucherConfig) voucherSummary {
	index := buildVoucherIndex(config)
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
	return summary
}

// buildVoucherIndex maps every configured alias to its voucher definition.
func buildVoucherIndex(config *voucherConfig) map[string]voucherDef {
	index := make(map[string]voucherDef)
	if config == nil {
		return index
	}
	for _, def := range config.Vouchers {
		aliases := append([]string{def.Name}, def.Names...)
		for _, alias := range aliases {
			key := normalizeName(alias)
			if key == "" {
				continue
			}
			index[key] = def
		}
	}
	return index
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
	reservedOriginiumValue := param.ReservedOriginium * param.OriginiumToOroberyl
	usableOriginium := values.Originium - reservedOriginiumValue
	if usableOriginium < 0 {
		usableOriginium = 0
	}

	resourcePulls := (values.Oroberyl + usableOriginium) / param.OroberylPerPull
	currentPoolTotal := resourcePulls + summary.CurrentOnlyPulls + summary.CarryToNextPulls
	nextPoolTotal := resourcePulls + summary.CarryToNextPulls + summary.NextOnlyPulls + param.NextPoolShopPulls + param.NextPoolSigninPulls

	return calculationResult{
		ReservedOriginium:      param.ReservedOriginium,
		ReservedOriginiumValue: reservedOriginiumValue,
		UsableOriginium:        usableOriginium,
		ResourcePulls:          resourcePulls,
		CurrentOnlyPulls:       summary.CurrentOnlyPulls,
		CarryToNextPulls:       summary.CarryToNextPulls,
		NextOnlyPulls:          summary.NextOnlyPulls,
		NextPoolShopPulls:      param.NextPoolShopPulls,
		NextPoolSigninPulls:    param.NextPoolSigninPulls,
		CurrentPoolTotal:       currentPoolTotal,
		NextPoolTotal:          nextPoolTotal,
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
		values.Originium,
		result.ReservedOriginium,
		result.ReservedOriginiumValue,
		result.UsableOriginium,
	)
	if len(summary.UnknownNames) > 0 {
		message += "\n" + i18n.T("pullcount.result.unknown_vouchers", strings.Join(summary.UnknownNames, i18n.Separator()))
	}
	return message
}
