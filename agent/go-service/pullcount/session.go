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

type actionParam struct {
	Stage string `json:"stage"`
	Cell  int    `json:"cell"`

	VoucherConfigPath string `json:"voucher_config_path"`
	WarehouseScanPath string `json:"warehouse_scan_path"`

	ReservedOriginium   int `json:"reserved_originium"`
	OriginiumToOroberyl int `json:"originium_to_oroberyl"`
	OroberylPerPull     int `json:"oroberyl_per_pull"`
	NextPoolShopPulls   int `json:"next_pool_shop_pulls"`
	NextPoolSigninPulls int `json:"next_pool_signin_pulls"`
}

type runSession struct {
	Param        actionParam
	Config       *voucherConfig
	ScanConfig   *warehouseScanConfig
	VoucherIndex map[string]voucherDef
	Values       resourceValues

	HasConvertedOriginium bool
	HasOroberyl           bool

	CurrentPageCells  map[int]scannedCell
	VoucherQuantities map[string]int
	LastHeadProbe     map[int]int
	CurrentProbe      map[int]int
	LastPageSignature map[int]int
	StopAfterPageDone bool
	PageStopReason    string

	PageCount        int
	CurrentCell      int
	CurrentProbeCell int
}

// requireSession returns the active run session or reports a user-facing error.
func requireSession(ctx *maa.Context) (*runSession, bool) {
	if currentSession != nil {
		return currentSession, true
	}
	err := fmt.Errorf("pull count session is not initialized")
	log.Error().Err(err).Str("component", componentName).Msg("missing session")
	maafocus.Print(ctx, i18n.T("pullcount.error.invalid_params"))
	return nil, false
}

// overrideNext changes a generic scheduler node to the concrete next node chosen by Go state.
func overrideNext(ctx *maa.Context, currentNode string, nextNode string, errorMessage string) bool {
	if err := ctx.OverrideNext(currentNode, []maa.NextItem{{Name: nextNode}}); err != nil {
		log.Error().Err(err).Str("component", componentName).Str("current", currentNode).Str("next", nextNode).Msg(errorMessage)
		maafocus.Print(ctx, i18n.T("pullcount.error.warehouse_scan_failed", err.Error()))
		return false
	}
	return true
}
