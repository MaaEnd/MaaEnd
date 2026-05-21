package pullcount

import (
	"fmt"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/maafocus"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

// --- Resource And Finish Stages --- //

// handleInit loads voucher and warehouse configuration, then starts a fresh scan session.
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
	scanConfig, err := loadWarehouseScanConfig(param.WarehouseScanPath)
	if err != nil {
		log.Error().Err(err).Str("component", componentName).Str("path", param.WarehouseScanPath).Msg("failed to load warehouse scan config")
		maafocus.Print(ctx, i18n.T("pullcount.error.config_failed", err.Error()))
		return false
	}

	currentSession = &runSession{
		Param:             *param,
		Config:            config,
		ScanConfig:        scanConfig,
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
