package pullcount

import (
	"fmt"
	"sync"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/ims"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/iconqty"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/maafocus"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

var (
	sessionMu      sync.Mutex
	currentSession *runSession
)

type runSession struct {
	Items map[string]int
}

func handleInit(ctx *maa.Context) bool {
	currentSession = newRunSession()
	log.Info().Str("component", componentName).Msg("pull count session initialized")
	return true
}

func newRunSession() *runSession {
	return &runSession{
		Items: make(map[string]int),
	}
}

func requireSession(ctx *maa.Context) (*runSession, bool) {
	if currentSession != nil {
		return currentSession, true
	}
	log.Error().Str("component", componentName).Msg("missing session")
	maafocus.Print(ctx, i18n.T("pullcount.error.invalid_params"))
	return nil, false
}

func (s *runSession) quantity(itemID string) int {
	if s != nil {
		if qty, ok := s.Items[itemID]; ok {
			return qty
		}
	}
	return ims.ItemQuantity(itemID)
}

func (s *runSession) hasRecorded(itemID string) bool {
	if s == nil {
		return false
	}
	_, ok := s.Items[itemID]
	return ok
}

// handleRecord runs each items node (And + box_index → OCR digit) and stores quantities.
func handleRecord(ctx *maa.Context, params actionParam) bool {
	session, ok := requireSession(ctx)
	if !ok {
		return false
	}

	img, err := cacheScreenImage(ctx)
	if err != nil {
		log.Error().
			Err(err).
			Str("component", componentName).
			Msg("failed to cache screen image")
		maafocus.Print(ctx, i18n.T("pullcount.error.recognition_failed", err.Error()))
		return false
	}

	for _, itemID := range sortedItemIDs(params.Items) {
		node := params.Items[itemID]
		displayName := iconqty.ItemDisplayName(itemID)
		qty, hit, err := readQuantityFromNode(ctx, img, node)
		if err != nil {
			log.Warn().
				Err(err).
				Str("component", componentName).
				Str("item_id", itemID).
				Str("node", node).
				Msg("failed to read quantity")
			maafocus.Print(ctx, i18n.T("pullcount.error.recognition_failed", fmt.Sprintf("%s: %s", displayName, err.Error())))
			return false
		}
		if !hit {
			if !params.Optional {
				log.Warn().
					Str("component", componentName).
					Str("item_id", itemID).
					Str("node", node).
					Msg("required recognizer not hit")
				maafocus.Print(ctx, i18n.T("pullcount.error.recognition_failed", displayName))
				return false
			}
			qty = 0
		}
		session.Items[itemID] = qty
		log.Info().
			Str("component", componentName).
			Str("item_id", itemID).
			Str("item_name", displayName).
			Str("node", node).
			Int("quantity", qty).
			Bool("hit", hit).
			Bool("optional", params.Optional).
			Msg("item quantity recorded")
		if hit {
			maafocus.Print(ctx, i18n.T("pullcount.resource_read_success", displayName, qty))
		}
	}
	return true
}

func handleFinish(ctx *maa.Context) bool {
	session, ok := requireSession(ctx)
	if !ok {
		return false
	}
	defer func() {
		currentSession = nil
	}()

	if err := ims.EnsureHydrated(); err != nil {
		log.Error().
			Err(err).
			Str("component", componentName).
			Msg("failed to hydrate ims cache")
		maafocus.Print(ctx, i18n.T("pullcount.error.recognition_failed", err.Error()))
		return false
	}

	hasDiamond := session.hasRecorded(itemDiamond) || ims.HasData()
	hasOriginium := session.hasRecorded(itemOriginium) || ims.HasData()
	if !hasDiamond || !hasOriginium {
		err := fmt.Errorf("resource quantities are incomplete")
		log.Warn().
			Err(err).
			Str("component", componentName).
			Bool("has_diamond", hasDiamond).
			Bool("has_originium", hasOriginium).
			Msg("cannot finish pull count")
		maafocus.Print(ctx, i18n.T("pullcount.error.recognition_failed", err.Error()))
		return false
	}

	values := resourceValues{
		Originium: session.quantity(itemOriginium),
		Oroberyl:  session.quantity(itemDiamond),
	}
	summary := voucherSummary{
		CarryToNextPulls: sumVoucherPulls(session.quantity),
	}
	result := calculatePullCount(values, summary)
	maafocus.Print(ctx, formatResultFocus(values, result))
	logCalculation(session, values, summary, result)
	return true
}

func sumVoucherPulls(quantity func(string) int) int {
	total := 0
	for itemID, pulls := range voucherPulls {
		total += quantity(itemID) * pulls
	}
	return total
}
