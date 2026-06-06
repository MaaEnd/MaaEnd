package pullcount

import (
	"fmt"
	"sync"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/maafocus"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

// --- Session State --- //

var (
	sessionMu      sync.Mutex
	currentSession *runSession
)

type runSession struct {
	Values      resourceValues
	Vouchers    voucherSummary
	VoucherHits map[string]struct{}

	HasConvertedOriginium bool
	HasOroberyl           bool
}

type voucherSummary struct {
	CarryToNextPulls int
	DossierPulls     int
}

// --- Resource And Finish Stages --- //

// handleInit starts a fresh scan session.
func handleInit(ctx *maa.Context) bool {
	currentSession = newRunSession()
	log.Info().Str("component", componentName).Msg("pull count session initialized")
	return true
}

// newRunSession builds the mutable state used by Pipeline stages.
func newRunSession() *runSession {
	return &runSession{
		VoucherHits: make(map[string]struct{}),
	}
}

// requireSession returns the active run session or reports a user-facing error.
func requireSession(ctx *maa.Context) (*runSession, bool) {
	if currentSession != nil {
		return currentSession, true
	}
	log.Error().Str("component", componentName).Msg("missing session")
	maafocus.Print(ctx, i18n.T("pullcount.error.invalid_params"))
	return nil, false
}

// handleRecordResource stores one resource counter from the current Pipeline OCR result.
func handleRecordResource(ctx *maa.Context, arg *maa.CustomActionArg, resource string) bool {
	session, ok := requireSession(ctx)
	if !ok {
		return false
	}

	value, err := readIntegerFromRecognition(arg.RecognitionDetail)
	label := resourceLabel(resource)
	if err != nil {
		log.Warn().Err(err).Str("component", componentName).Str("resource", label).Msg("failed to read resource OCR")
		maafocus.Print(ctx, i18n.T("pullcount.error.recognition_failed", fmt.Sprintf("%s: %s", label, err.Error())))
		return false
	}

	switch resource {
	case resourceOriginium:
		session.Values.ConvertedOriginiumOroberyl = value
		session.HasConvertedOriginium = true
	case resourceOroberyl:
		session.Values.Oroberyl = value
		session.HasOroberyl = true
	case resourceBondQuota:
		session.Values.BondQuota = value
	default:
		log.Error().Str("component", componentName).Str("resource", resource).Msg("unknown resource")
		maafocus.Print(ctx, i18n.T("pullcount.error.invalid_params"))
		return false
	}

	log.Info().Str("component", componentName).Str("resource", label).Int("value", value).Msg("resource recorded")
	maafocus.Print(ctx, i18n.T("pullcount.resource_read_success", label, value))
	return true
}

// resourceLabel returns the localized display label for a top-bar resource.
func resourceLabel(resource string) string {
	switch resource {
	case resourceOriginium:
		return i18n.T("pullcount.resource.originium")
	case resourceBondQuota:
		return i18n.T("pullcount.resource.bond_quota")
	default:
		return i18n.T("pullcount.resource.oroberyl")
	}
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

	result := calculatePullCount(session.Values, session.Vouchers)
	maafocus.Print(ctx, formatResultFocus(session.Values, result))
	logCalculation(session, result)
	return true
}

// --- Warehouse Scan Stages --- //

// handleRecordVoucher stores one Pipeline-classified voucher for the selected template hit.
func handleRecordVoucher(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	session, ok := requireSession(ctx)
	if !ok {
		return false
	}

	kind := voucherKindFromRecognition(arg.RecognitionDetail)
	added := recordCarryToNextVoucher(session, voucherKey(arg), kind)
	log.Info().
		Str("component", componentName).
		Bool("added", added).
		Str("kind", kind).
		Int("pulls", voucherPulls(kind)).
		Int("carry_to_next_pulls", session.Vouchers.CarryToNextPulls).
		Int("dossier_pulls", session.Vouchers.DossierPulls).
		Msg("warehouse voucher recorded")
	return true
}

// voucherPulls returns how many pulls the confirmed warehouse item contributes.
func voucherPulls(kind string) int {
	if kind == voucherKindDossier {
		return dossierPulls
	}
	return 1
}

// voucherKey builds a stable duplicate key from the template hit box passed by Pipeline.
func voucherKey(arg *maa.CustomActionArg) string {
	if arg == nil {
		return "carry_to_next"
	}
	box := arg.Box
	return fmt.Sprintf("%d:%d:%d:%d", box.X(), box.Y(), box.Width(), box.Height())
}

// recordCarryToNextVoucher adds one confirmed carry-over item unless the hit box was already counted.
func recordCarryToNextVoucher(session *runSession, key string, kind string) bool {
	if _, exists := session.VoucherHits[key]; exists {
		return false
	}
	session.VoucherHits[key] = struct{}{}
	if kind == voucherKindDossier {
		session.Vouchers.DossierPulls += dossierPulls
	} else {
		session.Vouchers.CarryToNextPulls++
	}
	return true
}
