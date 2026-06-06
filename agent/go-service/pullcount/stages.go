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

// handleRecordVoucher scans the current warehouse page and records all carry-over items on it.
func handleRecordVoucher(ctx *maa.Context, _ *maa.CustomActionArg) bool {
	session, ok := requireSession(ctx)
	if !ok {
		return false
	}

	hits, err := scanVoucherPage(ctx)
	if err != nil {
		log.Warn().Err(err).Str("component", componentName).Msg("failed to scan warehouse vouchers")
		maafocus.Print(ctx, i18n.T("pullcount.error.recognition_failed", err.Error()))
		return false
	}

	for _, hit := range hits {
		pulls := voucherPulls(hit.Kind, hit.Quantity)
		added := recordWarehousePullItem(session, voucherKey(hit.Box), hit.Kind, hit.Quantity)
		log.Info().
			Str("component", componentName).
			Bool("added", added).
			Str("kind", hit.Kind).
			Int("quantity", hit.Quantity).
			Int("pulls", pulls).
			Interface("box", hit.Box).
			Int("bond_quota", session.Values.BondQuota).
			Int("carry_to_next_pulls", session.Vouchers.CarryToNextPulls).
			Int("dossier_pulls", session.Vouchers.DossierPulls).
			Msg("warehouse pull item recorded")
	}

	if len(hits) == 0 {
		log.Info().Str("component", componentName).Msg("no warehouse pull items found on current page")
	}
	return true
}

// voucherPulls returns how many pulls the confirmed warehouse item contributes by quantity.
func voucherPulls(kind string, quantity int) int {
	if quantity <= 0 {
		return 0
	}
	switch kind {
	case voucherKindDossier:
		return quantity * dossierPulls
	case voucherKindBondQuota:
		return quantity / bondQuotaPerPull
	default:
		return quantity
	}
}

// voucherKey builds a stable duplicate key from a template hit box on the scanned page.
func voucherKey(box maa.Rect) string {
	return fmt.Sprintf("%d:%d:%d:%d", box.X(), box.Y(), box.Width(), box.Height())
}

// recordWarehousePullItem adds one confirmed warehouse pull item unless the hit box was already counted.
func recordWarehousePullItem(session *runSession, key string, kind string, quantity int) bool {
	if quantity <= 0 {
		return false
	}
	if _, exists := session.VoucherHits[key]; exists {
		return false
	}
	session.VoucherHits[key] = struct{}{}
	switch kind {
	case voucherKindDossier:
		session.Vouchers.DossierPulls += voucherPulls(kind, quantity)
	case voucherKindBondQuota:
		session.Values.BondQuota = quantity
	default:
		session.Vouchers.CarryToNextPulls += voucherPulls(kind, quantity)
	}
	return true
}
