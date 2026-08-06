package pullcount

import (
	"encoding/json"
	"strings"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/ims"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/maafocus"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const componentName = "PullCountCalculator"

const (
	defaultReserveOriginium = 29
	defaultReserveOroberyl  = 0
	originiumToOroberyl     = 75
	oroberylPerPull         = 500
	nextPoolShopPulls       = 5
	nextPoolSigninPulls     = 5
)

var _ maa.CustomActionRunner = &Action{}

// Action reads IMS shop/valuables stock and attach reserves, then prints pull totals.
type Action struct{}

type pullCountAttach struct {
	ReserveOriginium *int `json:"reserve_originium"`
	ReserveOroberyl  *int `json:"reserve_oroberyl"`
}

// Run implements maa.CustomActionRunner.
func (a *Action) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if ctx == nil || arg == nil {
		log.Error().Str("component", componentName).Msg("nil context or arg")
		return false
	}

	attach, err := loadPullCountAttach(ctx, arg.CurrentTaskName)
	if err != nil {
		log.Error().
			Err(err).
			Str("component", componentName).
			Str("node", arg.CurrentTaskName).
			Msg("failed to load attach")
		maafocus.Print(ctx, i18n.T("pullcount.error.invalid_params"))
		return false
	}

	if err := ims.EnsureHydrated(); err != nil {
		log.Error().
			Err(err).
			Str("component", componentName).
			Msg("failed to hydrate ims cache")
		maafocus.Print(ctx, i18n.T("pullcount.error.ims_not_ready"))
		return false
	}

	reserveOriginium := defaultReserveOriginium
	if attach.ReserveOriginium != nil {
		if *attach.ReserveOriginium < 0 {
			maafocus.Print(ctx, i18n.T("pullcount.error.invalid_reserve"))
			return false
		}
		reserveOriginium = *attach.ReserveOriginium
	}
	reserveOroberyl := defaultReserveOroberyl
	if attach.ReserveOroberyl != nil {
		if *attach.ReserveOroberyl < 0 {
			maafocus.Print(ctx, i18n.T("pullcount.error.invalid_reserve"))
			return false
		}
		reserveOroberyl = *attach.ReserveOroberyl
	}

	stock := resourceStock{
		Originium:        ims.ItemQuantity("ORIGEOMETRY"),
		Oroberyl:         ims.ItemQuantity("OROBERYL"),
		CarryToNextPulls: ims.ItemQuantity("CHARTERED_HH_PERMIT"),
	}
	result := calculatePullCount(stock, reserveOriginium, reserveOroberyl)
	maafocus.Print(ctx, formatResultFocus(stock, result))
	log.Info().
		Str("component", componentName).
		Interface("stock", stock).
		Int("reserve_originium", reserveOriginium).
		Int("reserve_oroberyl", reserveOroberyl).
		Interface("result", result).
		Msg("pull count calculated")
	return true
}

func loadPullCountAttach(ctx *maa.Context, nodeName string) (pullCountAttach, error) {
	if ctx == nil || strings.TrimSpace(nodeName) == "" {
		return pullCountAttach{}, nil
	}
	raw, err := ctx.GetNodeJSON(nodeName)
	if err != nil {
		return pullCountAttach{}, err
	}
	var wrapper struct {
		Attach pullCountAttach `json:"attach"`
	}
	if err := json.Unmarshal([]byte(raw), &wrapper); err != nil {
		return pullCountAttach{}, err
	}
	return wrapper.Attach, nil
}
