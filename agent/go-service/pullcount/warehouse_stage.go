package pullcount

import (
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/maafocus"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

// --- Warehouse Scan Stages --- //

// handleRecordVoucher stores one Pipeline-classified voucher for the selected template hit.
func handleRecordVoucher(ctx *maa.Context, arg *maa.CustomActionArg, param *actionParam) bool {
	session, ok := requireSession(ctx)
	if !ok {
		return false
	}

	quantity, added, err := addVoucher(session, voucherKey(arg, param.PoolScope), param.PoolScope, param.PullValue)
	if err != nil {
		log.Error().Err(err).Str("component", componentName).Msg("invalid voucher record")
		maafocus.Print(ctx, i18n.T("pullcount.error.invalid_params"))
		return false
	}

	log.Debug().
		Str("component", componentName).
		Str("pool_scope", param.PoolScope).
		Int("pull_value", param.PullValue).
		Int("quantity", quantity).
		Bool("added", added).
		Msg("warehouse voucher recorded")
	return true
}

// handlePageDone advances the page counter and lets Pipeline choose whether to finish or scroll.
func handlePageDone(ctx *maa.Context) bool {
	session, ok := requireSession(ctx)
	if !ok {
		return false
	}

	session.PageCount++
	session.PendingQuantity = make(map[string]int)
	session.StopAfterPageDone = session.PageCount >= session.Param.ScanMaxPages
	session.PageStopReason = ""
	if session.StopAfterPageDone {
		session.PageStopReason = "warehouse scan reached max pages"
	}
	log.Info().
		Str("component", componentName).
		Int("page_count", session.PageCount).
		Str("stop_reason", session.PageStopReason).
		Msg("warehouse page scan done")
	return true
}

// handleQuantityOCR stores the quantity OCR for the voucher selected by Pipeline.
func handleQuantityOCR(ctx *maa.Context, arg *maa.CustomActionArg, poolScope string) bool {
	session, ok := requireSession(ctx)
	if !ok {
		return false
	}
	quantity, err := readIntegerFromRecognition(arg.RecognitionDetail)
	if err != nil || quantity <= 0 {
		log.Debug().Err(err).Str("component", componentName).Msg("warehouse quantity OCR ignored")
		return true
	}
	if session.PendingQuantity == nil {
		session.PendingQuantity = make(map[string]int)
	}
	session.PendingQuantity[poolScope] = quantity
	log.Debug().Str("component", componentName).Str("pool_scope", poolScope).Int("quantity", quantity).Msg("warehouse quantity recorded")
	return true
}
