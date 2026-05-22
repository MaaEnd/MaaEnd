package pullcount

import (
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

type actionParam struct {
	Stage     string `json:"stage"`
	PoolScope string `json:"pool_scope"`
	PullValue int    `json:"pull_value"`

	ReservedOriginium   int `json:"reserved_originium"`
	OriginiumToOroberyl int `json:"originium_to_oroberyl"`
	OroberylPerPull     int `json:"oroberyl_per_pull"`
	NextPoolShopPulls   int `json:"next_pool_shop_pulls"`
	NextPoolSigninPulls int `json:"next_pool_signin_pulls"`

	ScanMaxPages int `json:"scan_max_pages"`
}

type runSession struct {
	Param       actionParam
	Values      resourceValues
	Vouchers    voucherSummary
	VoucherHits map[string]struct{}

	HasConvertedOriginium bool
	HasOroberyl           bool

	PendingQuantity   map[string]int
	StopAfterPageDone bool
	PageStopReason    string

	PageCount int
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
