package pullcount

import (
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/maafocus"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

// --- Warehouse Scan Stages --- //

// handleRecordVoucher stores one Pipeline-classified voucher for the selected cell.
func handleRecordVoucher(ctx *maa.Context, param *actionParam) bool {
	session, ok := requireSession(ctx)
	if !ok {
		return false
	}

	quantity, added, err := addVoucher(session, param.Cell, param.PoolScope, param.PullValue)
	if err != nil {
		log.Error().Err(err).Str("component", componentName).Msg("invalid voucher record")
		maafocus.Print(ctx, i18n.T("pullcount.error.invalid_params"))
		return false
	}

	log.Debug().
		Str("component", componentName).
		Int("cell", param.Cell).
		Str("pool_scope", param.PoolScope).
		Int("pull_value", param.PullValue).
		Int("quantity", quantity).
		Bool("added", added).
		Msg("warehouse voucher recorded")
	return true
}

// handleScanBegin clears transient state before scanning a page or scroll probe.
func handleScanBegin(ctx *maa.Context, probe bool) bool {
	session, ok := requireSession(ctx)
	if !ok {
		return false
	}

	if probe {
		session.CurrentProbe = make(map[int]int)
	} else {
		session.CurrentPageCells = make(map[int]scannedCell)
		session.CurrentProbe = nil
	}
	log.Debug().Str("component", componentName).Int("page", session.PageCount+1).Bool("probe", probe).Msg("warehouse scan begin")
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
		Str("stop_reason", session.PageStopReason).
		Msg("warehouse page scan done")
	return true
}

// handleQuantityOCR stores quantity OCR for either a grid cell or a scroll probe.
func handleQuantityOCR(ctx *maa.Context, arg *maa.CustomActionArg, cell int, probe bool) bool {
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
		log.Debug().Err(err).Str("component", componentName).Int("cell", cell).Bool("probe", probe).Msg("warehouse quantity OCR ignored")
		return true
	}
	if probe {
		if session.CurrentProbe == nil {
			session.CurrentProbe = make(map[int]int)
		}
		session.CurrentProbe[cell] = quantity
	} else {
		recordPageQuantity(session, cell, quantity)
	}
	log.Debug().Str("component", componentName).Int("cell", cell).Int("quantity", quantity).Bool("probe", probe).Msg("warehouse quantity recorded")
	return true
}
