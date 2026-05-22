package pullcount

import (
	"fmt"
)

// --- Warehouse Page State --- //

type scannedCell struct {
	Quantity    int
	HasQuantity bool
}

type warehouseScanConfig struct {
	Probe        warehouseSimilarityRule `json:"probe"`
	RepeatPage   warehouseSimilarityRule `json:"repeat_page"`
	ScanMaxPages int                     `json:"scan_max_pages"`
}

type warehouseSimilarityRule struct {
	CellLimit     int     `json:"cell_limit"`
	MinComparable int     `json:"min_comparable"`
	MaxMismatches int     `json:"max_mismatches"`
	MinMatchRatio float64 `json:"min_match_ratio"`
}

// validate rejects incomplete scan-stop parameters instead of guessing runtime defaults.
func (config *warehouseScanConfig) validate() error {
	if config == nil {
		return fmt.Errorf("warehouse scan config is nil")
	}
	if err := validateSimilarityRule("probe", &config.Probe); err != nil {
		return err
	}
	if err := validateSimilarityRule("repeat_page", &config.RepeatPage); err != nil {
		return err
	}
	if config.ScanMaxPages <= 0 {
		return fmt.Errorf("scan_max_pages must be positive")
	}
	return nil
}

// scanConfig returns the warehouse stop thresholds embedded in the init action params.
func (param *actionParam) scanConfig() warehouseScanConfig {
	return warehouseScanConfig{
		Probe:        param.Probe,
		RepeatPage:   param.RepeatPage,
		ScanMaxPages: param.ScanMaxPages,
	}
}

// validateSimilarityRule checks one fuzzy page comparison rule.
func validateSimilarityRule(name string, rule *warehouseSimilarityRule) error {
	if rule.MinComparable <= 0 || rule.MinComparable > rule.CellLimit {
		return fmt.Errorf("%s.min_comparable must be between 1 and cell_limit", name)
	}
	if rule.CellLimit <= 0 || rule.MaxMismatches < 0 || rule.MinMatchRatio <= 0 || rule.MinMatchRatio > 1 {
		return fmt.Errorf("%s has invalid similarity thresholds", name)
	}
	return nil
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

// recordVisiblePage accumulates recognized vouchers and stores the top-row probe for the next scroll.
func recordVisiblePage(session *runSession) int {
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
	session.LastHeadProbe = quantitySignatureFromCells(session.CurrentPageCells, session.ScanConfig.Probe.CellLimit)
	session.PageCount++
	return len(session.CurrentPageCells)
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
	return float64(matches)/float64(comparable) >= rule.MinMatchRatio, comparable, matches
}
