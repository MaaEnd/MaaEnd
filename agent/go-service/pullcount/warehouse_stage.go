package pullcount

import (
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/maafocus"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

// --- Warehouse Scan Stages --- //

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
	session.CurrentCell = 1
	session.CurrentProbeCell = 0
	log.Debug().Str("component", componentName).Int("page", session.PageCount+1).Msg("warehouse page scan begin")
	return true
}

// handleCellPrepare configures generic Pipeline nodes for the current warehouse cell.
func handleCellPrepare(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	session, ok := requireSession(ctx)
	if !ok {
		return false
	}
	if session.CurrentCell <= 0 {
		session.CurrentCell = 1
	}
	if session.CurrentCell > session.ScanConfig.CellCount() {
		return overrideNext(ctx, arg.CurrentTaskName, "PullCountCalculatorPageDone", "cell prepare page done")
	}

	if err := overrideCellScanNodes(ctx, session.ScanConfig, session.CurrentCell); err != nil {
		log.Error().Err(err).Str("component", componentName).Int("cell", session.CurrentCell).Msg("failed to override cell scan nodes")
		maafocus.Print(ctx, i18n.T("pullcount.error.warehouse_scan_failed", err.Error()))
		return false
	}

	log.Debug().Str("component", componentName).Int("page", session.PageCount+1).Int("cell", session.CurrentCell).Msg("warehouse cell scan prepared")
	return true
}

// handleCellAdvance moves to the next warehouse cell or finishes the current page.
func handleCellAdvance(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	session, ok := requireSession(ctx)
	if !ok {
		return false
	}

	session.CurrentCell++
	nextNode := "PullCountCalculatorCellPrepare"
	if session.CurrentCell > session.ScanConfig.CellCount() {
		nextNode = "PullCountCalculatorPageDone"
	}
	if !overrideNext(ctx, arg.CurrentTaskName, nextNode, "warehouse cell scan advanced") {
		return false
	}

	log.Debug().Str("component", componentName).Int("next_cell", session.CurrentCell).Str("next", nextNode).Msg("warehouse cell scan advanced")
	return true
}

// handlePageDone records the scanned page and decides whether to scroll or finish.
func handlePageDone(ctx *maa.Context) bool {
	session, ok := requireSession(ctx)
	if !ok {
		return false
	}

	items := recordVisiblePage(session)
	nextNode := nextWarehouseScrollNode
	if session.StopAfterPageDone {
		nextNode = nextFinishNode
	}

	if !overrideNext(ctx, "PullCountCalculatorPageDone", nextNode, "failed to override page done next") {
		return false
	}
	log.Info().
		Str("component", componentName).
		Int("page_count", session.PageCount).
		Int("items", items).
		Bool("stop_after_page_done", session.StopAfterPageDone).
		Str("stop_reason", session.PageStopReason).
		Str("next", nextNode).
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
	session.CurrentProbeCell = 1
	log.Debug().Str("component", componentName).Int("page", session.PageCount+1).Msg("warehouse scroll probe begin")
	return true
}

// handleProbePrepare configures the generic post-scroll quantity probe node.
func handleProbePrepare(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	session, ok := requireSession(ctx)
	if !ok {
		return false
	}
	if session.CurrentProbeCell <= 0 {
		session.CurrentProbeCell = 1
	}
	if session.CurrentProbeCell > session.ScanConfig.Probe.CellLimit {
		return overrideNext(ctx, arg.CurrentTaskName, "PullCountCalculatorScrollProbeDone", "probe prepare done")
	}

	if err := overrideProbeNode(ctx, session.ScanConfig, session.CurrentProbeCell); err != nil {
		log.Error().Err(err).Str("component", componentName).Int("cell", session.CurrentProbeCell).Msg("failed to override scroll probe node")
		maafocus.Print(ctx, i18n.T("pullcount.error.warehouse_scan_failed", err.Error()))
		return false
	}

	log.Debug().Str("component", componentName).Int("cell", session.CurrentProbeCell).Msg("warehouse scroll probe prepared")
	return true
}

// handleProbeAdvance moves to the next post-scroll probe cell or ends probing.
func handleProbeAdvance(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	session, ok := requireSession(ctx)
	if !ok {
		return false
	}

	session.CurrentProbeCell++
	nextNode := "PullCountCalculatorScrollProbePrepare"
	if session.CurrentProbeCell > session.ScanConfig.Probe.CellLimit {
		nextNode = "PullCountCalculatorScrollProbeDone"
	}
	if !overrideNext(ctx, arg.CurrentTaskName, nextNode, "warehouse scroll probe advanced") {
		return false
	}

	log.Debug().Str("component", componentName).Int("next_probe_cell", session.CurrentProbeCell).Str("next", nextNode).Msg("warehouse scroll probe advanced")
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

	unchanged, comparable, matches := scrollProbeUnchanged(session.ScanConfig.Probe, session.LastHeadProbe, session.CurrentProbe)
	nextNode := nextPageBeginNode
	reason := "scroll probe changed"
	if unchanged {
		nextNode = nextFinishNode
		reason = "warehouse scan reached bottom / probe mostly unchanged"
	}

	if !overrideNext(ctx, arg.CurrentTaskName, nextNode, "failed to override scroll probe next") {
		return false
	}

	log.Info().
		Str("component", componentName).
		Int("comparable", comparable).
		Int("matches", matches).
		Ints("mismatch_cells", probeMismatchCells(session.LastHeadProbe, session.CurrentProbe)).
		Float64("match_ratio", matchRatio(comparable, matches)).
		Float64("min_match_ratio", session.ScanConfig.Probe.MinMatchRatio).
		Int("max_mismatches", session.ScanConfig.Probe.MaxMismatches).
		Interface("before_probe", session.LastHeadProbe).
		Interface("after_probe", session.CurrentProbe).
		Bool("unchanged", unchanged).
		Str("next", nextNode).
		Msg(reason)
	return true
}
