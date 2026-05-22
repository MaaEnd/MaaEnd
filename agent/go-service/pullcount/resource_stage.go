package pullcount

import (
	"fmt"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/maafocus"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

// --- Resource And Finish Stages --- //

// handleInit starts a fresh scan session with Pipeline-provided thresholds.
func handleInit(ctx *maa.Context, param *actionParam) bool {
	session, err := newRunSession(param)
	if err != nil {
		log.Error().Err(err).Str("component", componentName).Msg("failed to initialize pull count session")
		maafocus.Print(ctx, i18n.T("pullcount.error.config_failed", err.Error()))
		return false
	}
	currentSession = session
	log.Info().Str("component", componentName).Msg("pull count session initialized")
	return true
}

// newRunSession builds the mutable state used by Pipeline stages.
func newRunSession(param *actionParam) (*runSession, error) {
	return &runSession{
		Param:       *param,
		VoucherHits: make(map[string]struct{}),
	}, nil
}

// handleRecordResource stores one resource counter from the current Pipeline OCR result.
func handleRecordResource(ctx *maa.Context, arg *maa.CustomActionArg, convertedOriginium bool) bool {
	session, ok := requireSession(ctx)
	if !ok {
		return false
	}

	value, err := readIntegerFromRecognition(arg.RecognitionDetail)
	label := i18n.T("pullcount.resource.oroberyl")
	if convertedOriginium {
		label = i18n.T("pullcount.resource.originium")
	}
	if err != nil {
		log.Warn().Err(err).Str("component", componentName).Str("resource", label).Msg("failed to read resource OCR")
		maafocus.Print(ctx, i18n.T("pullcount.error.recognition_failed", fmt.Sprintf("%s: %s", label, err.Error())))
		return false
	}

	if convertedOriginium {
		session.Values.ConvertedOriginiumOroberyl = value
		session.HasConvertedOriginium = true
	} else {
		session.Values.Oroberyl = value
		session.HasOroberyl = true
	}

	log.Info().Str("component", componentName).Str("resource", label).Int("value", value).Msg("resource recorded")
	maafocus.Print(ctx, i18n.T("pullcount.resource_read_success", label, value))
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

	result := calculatePullCount(session.Values, session.Vouchers, &session.Param)
	maafocus.Print(ctx, formatResultFocus(session.Values, result))
	logCalculation(session, result)
	return true
}
