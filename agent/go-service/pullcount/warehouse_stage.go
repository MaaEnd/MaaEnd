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
	log.Debug().Str("component", componentName).Int("page", session.PageCount+1).Msg("warehouse page scan begin")
	return true
}

// handlePageDone records the scanned page and leaves the next branch to Pipeline recognition nodes.
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
		Bool("stop_after_page_done", session.StopAfterPageDone).
		Str("stop_reason", session.PageStopReason).
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
