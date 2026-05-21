package pullcount

import (
	"fmt"
	"math"
	"sort"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/resource"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

// --- Warehouse Page State --- //

const (
	nodeCellClick       = "PullCountCalculatorCellClick"
	nodeCellPresent     = "PullCountCalculatorCellPresent"
	nodeCellQuantityOCR = "PullCountCalculatorCellQuantityOCR"
	nodeCellTitleOCR    = "PullCountCalculatorCellTitleOCR"
	nodeProbeQuantity   = "PullCountCalculatorScrollProbeQuantityOCR"
)

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

type cellGeometry struct {
	Cell        int
	PresentROI  []int
	ClickTarget []int
	QuantityROI []int
	TitleROI    []int
}

type warehouseScanConfig struct {
	Grid              warehouseGridConfig     `json:"grid"`
	QuantityROIOffset []int                   `json:"quantity_roi_offset"`
	TitleROI          []int                   `json:"title_roi"`
	TitleExpected     []string                `json:"title_expected"`
	Probe             warehouseSimilarityRule `json:"probe"`
	RepeatPage        warehouseSimilarityRule `json:"repeat_page"`
	ScanMaxPages      int                     `json:"scan_max_pages"`
}

type warehouseGridConfig struct {
	Columns  int   `json:"columns"`
	Rows     int   `json:"rows"`
	Start    []int `json:"start"`
	Step     []int `json:"step"`
	CellSize []int `json:"cell_size"`
}

type warehouseSimilarityRule struct {
	CellLimit     int     `json:"cell_limit"`
	MinComparable int     `json:"min_comparable"`
	MaxMismatches int     `json:"max_mismatches"`
	MinMatchRatio float64 `json:"min_match_ratio"`
}

// loadWarehouseScanConfig reads and validates warehouse scan geometry from assets data.
func loadWarehouseScanConfig(path string) (*warehouseScanConfig, error) {
	var config warehouseScanConfig
	if err := resource.ReadJsonResource(path, &config); err != nil {
		return nil, err
	}
	if err := config.validate(); err != nil {
		return nil, err
	}
	return &config, nil
}

// CellCount returns the number of warehouse cells covered by the configured grid.
func (config *warehouseScanConfig) CellCount() int {
	if config == nil {
		return 0
	}
	return config.Grid.Columns * config.Grid.Rows
}

// validate rejects incomplete scan geometry instead of guessing runtime defaults.
func (config *warehouseScanConfig) validate() error {
	if config == nil {
		return fmt.Errorf("warehouse scan config is nil")
	}
	if config.Grid.Columns <= 0 || config.Grid.Rows <= 0 {
		return fmt.Errorf("grid columns and rows must be positive")
	}
	if len(config.Grid.Start) != 2 {
		return fmt.Errorf("grid.start must contain 2 numbers")
	}
	if len(config.Grid.Step) != 2 {
		return fmt.Errorf("grid.step must contain 2 numbers")
	}
	if len(config.Grid.CellSize) != 2 {
		return fmt.Errorf("grid.cell_size must contain 2 numbers")
	}
	if len(config.QuantityROIOffset) != 4 {
		return fmt.Errorf("quantity_roi_offset must contain 4 numbers")
	}
	if len(config.TitleROI) != 4 {
		return fmt.Errorf("title_roi must contain 4 numbers")
	}
	if len(config.TitleExpected) == 0 {
		return fmt.Errorf("title_expected must not be empty")
	}
	if err := validateSimilarityRule("probe", &config.Probe, config.Grid.Columns); err != nil {
		return err
	}
	if err := validateSimilarityRule("repeat_page", &config.RepeatPage, config.CellCount()); err != nil {
		return err
	}
	if config.ScanMaxPages <= 0 {
		return fmt.Errorf("scan_max_pages must be positive")
	}
	return nil
}

// validateSimilarityRule checks one fuzzy page comparison rule.
func validateSimilarityRule(name string, rule *warehouseSimilarityRule, maxCells int) error {
	if rule.CellLimit <= 0 || rule.CellLimit > maxCells {
		return fmt.Errorf("%s.cell_limit must be between 1 and %d", name, maxCells)
	}
	if rule.MinComparable <= 0 || rule.MinComparable > rule.CellLimit {
		return fmt.Errorf("%s.min_comparable must be between 1 and cell_limit", name)
	}
	if rule.MaxMismatches < 0 {
		return fmt.Errorf("%s.max_mismatches must be non-negative", name)
	}
	if rule.MinMatchRatio <= 0 || rule.MinMatchRatio > 1 {
		return fmt.Errorf("%s.min_match_ratio must be in (0, 1]", name)
	}
	return nil
}

// geometryForCell returns configured 720p-based rectangles for one warehouse grid cell.
func geometryForCell(config *warehouseScanConfig, cell int) (cellGeometry, error) {
	if config == nil {
		return cellGeometry{}, fmt.Errorf("warehouse scan config is nil")
	}
	cellCount := config.CellCount()
	if cell <= 0 || cell > cellCount {
		return cellGeometry{}, fmt.Errorf("cell %d out of range 1-%d", cell, cellCount)
	}

	index := cell - 1
	col := index % config.Grid.Columns
	row := index / config.Grid.Columns
	if row >= config.Grid.Rows {
		return cellGeometry{}, fmt.Errorf("cell %d row %d out of range", cell, row)
	}

	x := config.Grid.Start[0] + col*config.Grid.Step[0]
	y := config.Grid.Start[1] + row*config.Grid.Step[1]
	cellW := config.Grid.CellSize[0]
	cellH := config.Grid.CellSize[1]
	return cellGeometry{
		Cell:        cell,
		PresentROI:  []int{x, y, cellW, cellH},
		ClickTarget: []int{x + cellW/2, y + cellH/2, 1, 1},
		QuantityROI: []int{x + config.QuantityROIOffset[0], y + config.QuantityROIOffset[1], config.QuantityROIOffset[2], config.QuantityROIOffset[3]},
		TitleROI:    append([]int(nil), config.TitleROI...),
	}, nil
}

// overrideCellScanNodes points generic Pipeline nodes at one concrete warehouse cell.
func overrideCellScanNodes(ctx *maa.Context, config *warehouseScanConfig, cell int) error {
	geometry, err := geometryForCell(config, cell)
	if err != nil {
		return err
	}

	return ctx.OverridePipeline(buildCellScanOverride(geometry, config.TitleExpected))
}

// buildCellScanOverride creates the Pipeline patch for one concrete warehouse cell.
func buildCellScanOverride(geometry cellGeometry, titleExpected []string) map[string]any {
	recordQuantityParam := map[string]any{
		"stage": "record_quantity",
		"cell":  geometry.Cell,
	}
	recordItemParam := map[string]any{
		"stage": "record_item",
		"cell":  geometry.Cell,
	}

	return map[string]any{
		nodeCellPresent: map[string]any{
			"roi": append([]int(nil), geometry.PresentROI...),
		},
		nodeCellClick: map[string]any{
			"target": append([]int(nil), geometry.ClickTarget...),
		},
		nodeCellQuantityOCR: map[string]any{
			"roi": append([]int(nil), geometry.QuantityROI...),
			"custom_action_param": map[string]any{
				"stage": recordQuantityParam["stage"],
				"cell":  recordQuantityParam["cell"],
			},
		},
		nodeCellTitleOCR: map[string]any{
			"expected": append([]string(nil), titleExpected...),
			"roi":      append([]int(nil), geometry.TitleROI...),
			"custom_action_param": map[string]any{
				"stage": recordItemParam["stage"],
				"cell":  recordItemParam["cell"],
			},
		},
	}
}

// overrideProbeNode points the generic scroll probe OCR node at one top-row cell.
func overrideProbeNode(ctx *maa.Context, config *warehouseScanConfig, cell int) error {
	geometry, err := geometryForCell(config, cell)
	if err != nil {
		return err
	}

	return ctx.OverridePipeline(buildProbeOverride(geometry))
}

// buildProbeOverride creates the Pipeline patch for one concrete scroll-probe cell.
func buildProbeOverride(geometry cellGeometry) map[string]any {
	return map[string]any{
		nodeProbeQuantity: map[string]any{
			"roi": append([]int(nil), geometry.QuantityROI...),
			"custom_action_param": map[string]any{
				"stage": "record_probe_quantity",
				"cell":  geometry.Cell,
			},
		},
	}
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
func recordPageItem(session *runSession, cell int, title string) {
	if ignoredPageTitle(title) {
		return
	}
	if session.CurrentPageCells == nil {
		session.CurrentPageCells = make(map[int]scannedCell)
	}
	current := session.CurrentPageCells[cell]
	current.Name = title
	if current.Quantity <= 0 {
		current.Quantity = 1
		current.HasQuantity = false
	}
	session.CurrentPageCells[cell] = current
}

// recordVisiblePage accumulates recognized vouchers and stores the top-row probe for the next scroll.
func recordVisiblePage(session *runSession) int {
	items := currentPageItems(session)
	currentSignature := quantitySignatureFromCells(session.CurrentPageCells, session.ScanConfig.RepeatPage.CellLimit)
	repeated, _, _ := quantityVectorsMostlyUnchanged(session.ScanConfig.RepeatPage, session.LastPageSignature, currentSignature)
	session.StopAfterPageDone = false
	session.PageStopReason = ""
	if repeated {
		session.StopAfterPageDone = true
		session.PageStopReason = "warehouse scan reached bottom / repeated page signature"
	} else if session.PageCount+1 >= session.ScanConfig.ScanMaxPages {
		session.StopAfterPageDone = true
		session.PageStopReason = "warehouse scan reached max pages"
	}
	session.LastPageSignature = currentSignature
	session.LastHeadProbe = headQuantityProbeFromCells(session.CurrentPageCells, session.ScanConfig.Probe.CellLimit)
	accumulatePageVouchers(session, items)
	session.PageCount++
	return len(items)
}

// accumulatePageVouchers keeps the largest visible quantity for each configured voucher title.
func accumulatePageVouchers(session *runSession, items []pageItem) {
	for _, item := range items {
		if _, ok := session.VoucherIndex[normalizeName(item.Name)]; !ok {
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

// currentPageItems returns the visible warehouse page from cell-indexed Pipeline OCR records.
func currentPageItems(session *runSession) []pageItem {
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

// quantitySignatureFromCells returns the visible quantity vector used for repeated-page fallback.
func quantitySignatureFromCells(cells map[int]scannedCell, limit int) map[int]int {
	if limit <= 0 {
		return nil
	}
	signature := make(map[int]int)
	for cell, item := range cells {
		if cell <= 0 || cell > limit || !item.HasQuantity || item.Quantity <= 0 {
			continue
		}
		signature[cell] = item.Quantity
	}
	return signature
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
func scrollProbeUnchanged(rule warehouseSimilarityRule, before map[int]int, after map[int]int) (bool, int, int) {
	return quantityVectorsMostlyUnchanged(rule, before, after)
}

// quantityVectorsMostlyUnchanged allows small OCR noise while detecting an unchanged warehouse page.
func quantityVectorsMostlyUnchanged(rule warehouseSimilarityRule, before map[int]int, after map[int]int) (bool, int, int) {
	comparable := 0
	matches := 0
	for cell, beforeValue := range before {
		if rule.CellLimit > 0 && cell > rule.CellLimit {
			continue
		}
		afterValue, ok := after[cell]
		if !ok {
			continue
		}
		comparable++
		if beforeValue == afterValue {
			matches++
		}
	}
	if comparable < rule.MinComparable {
		return false, comparable, matches
	}
	mismatches := comparable - matches
	if mismatches > rule.MaxMismatches {
		return false, comparable, matches
	}
	return matchRatio(comparable, matches) >= rule.MinMatchRatio, comparable, matches
}

// matchRatio returns a stable match ratio for logging and similarity decisions.
func matchRatio(comparable int, matches int) float64 {
	if comparable <= 0 {
		return 0
	}
	ratio := float64(matches) / float64(comparable)
	return math.Round(ratio*1000) / 1000
}

// probeMismatchCells returns cells with changed or missing probe quantities for detailed logs.
func probeMismatchCells(before map[int]int, after map[int]int) []int {
	cells := make([]int, 0)
	for cell, beforeValue := range before {
		afterValue, ok := after[cell]
		if !ok || afterValue != beforeValue {
			cells = append(cells, cell)
		}
	}
	sort.Ints(cells)
	return cells
}
