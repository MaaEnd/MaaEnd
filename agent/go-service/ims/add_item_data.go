package ims

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/iconqty"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/maafocus"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const (
	componentAddItemData = "AddItemData"
	// Pipeline node that moves the cursor off reward icons; ADB overlays DoNothing.
	nodeIMSA3MouseMoveReset = "IMSA3MouseMoveReset"
)

var _ maa.CustomActionRunner = &AddItemData{}

// addItemDataParam is custom_action_param for AddItemData (A3).
//
// Same recognition path as SyncItemData (A2) via pkg/iconqty: one
// IconRecognition pass (grid_type defaults to rewards, deduplicate=false),
// then OCR quantity from each match cell_box.
type addItemDataParam struct {
	GridType    string   `json:"grid_type"`
	ROI         []int    `json:"roi"`
	ItemFilters []string `json:"item_filters"`
}

// AddItemData recognizes items on the current rewards screen and adds their
// OCR quantities into the IMS cache (A3). Does not change readiness / last_sync.
//
// If IMS has never been initialized (hasData=false), recognition still runs and
// per-item Focus is printed; cache write is skipped and the action returns
// success so Pipeline can continue (e.g. closing the rewards UI). No IMS
// init / summary Focus is printed in either case.
//
// Best practice: run as the action of a node that recognizes CloseRewardsButton,
// then next to a Click node that closes the rewards UI.
type AddItemData struct{}

// Run implements maa.CustomActionRunner.
func (a *AddItemData) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if arg == nil {
		log.Error().
			Str("component", componentAddItemData).
			Msg("nil custom action arg")
		return false
	}

	params, err := parseAddItemDataParam(arg.CustomActionParam)
	if err != nil {
		log.Error().
			Err(err).
			Str("component", componentAddItemData).
			Str("custom_action_param", arg.CustomActionParam).
			Msg("failed to parse params")
		return false
	}
	gridType := params.GridType
	if gridType == "" {
		gridType = iconqty.GridRewards
	}

	if err := ensureHydrated(); err != nil {
		log.Error().
			Err(err).
			Str("component", componentAddItemData).
			Msg("failed to hydrate ims cache")
		return false
	}

	cacheReady, _ := globalCache.snapshot()
	if !cacheReady {
		log.Info().
			Str("component", componentAddItemData).
			Msg("ims data not initialized, recognize and focus only, skip cache write")
	}

	if ctx == nil {
		log.Error().
			Str("component", componentAddItemData).
			Msg("nil context")
		return false
	}

	tasker := ctx.GetTasker()
	if tasker == nil || tasker.GetController() == nil {
		log.Error().
			Str("component", componentAddItemData).
			Msg("tasker or controller is nil")
		return false
	}
	ctrl := tasker.GetController()
	resetCursorBeforeRecognition(ctx, ctrl)
	img, err := ctrl.CacheImage()
	if err != nil || img == nil {
		log.Error().
			Err(err).
			Str("component", componentAddItemData).
			Msg("failed to cache image")
		return false
	}

	hits, err := iconqty.RecognizeQuantities(ctx, img, iconqty.Request{
		GridType:    gridType,
		ROI:         params.ROI,
		ItemFilters: params.ItemFilters,
		Deduplicate: false,
	})
	if err != nil {
		log.Error().
			Err(err).
			Str("component", componentAddItemData).
			Str("grid_type", gridType).
			Strs("item_filters", params.ItemFilters).
			Msg("failed to recognize reward icons")
		return false
	}

	addedTotal := 0
	applied := 0
	var (
		persistItems map[string]int
		lastSync     time.Time
		hasData      bool
	)
	for _, h := range hits {
		if h.Qty <= 0 {
			log.Info().
				Str("component", componentAddItemData).
				Str("item_id", h.ItemID).
				Int("quantity", h.Qty).
				Msg("non-positive quantity, skip")
			continue
		}
		displayName := iconqty.ItemDisplayName(h.ItemID)
		maafocus.Print(ctx, i18n.T("ims.add_item_found", displayName, h.Qty))
		addedTotal += h.Qty
		applied++

		if !cacheReady {
			log.Info().
				Str("component", componentAddItemData).
				Str("item_id", h.ItemID).
				Str("item_name", displayName).
				Int("delta", h.Qty).
				Bool("cache_ready", false).
				Msg("item recognized, skip cache write")
			continue
		}

		before, after, _, items, syncAt, ready := globalCache.applyDelta(h.ItemID, h.Qty)
		persistItems = items
		lastSync = syncAt
		hasData = ready
		log.Info().
			Str("component", componentAddItemData).
			Str("item_id", h.ItemID).
			Str("item_name", displayName).
			Int("delta", h.Qty).
			Int("before", before).
			Int("after", after).
			Msg("item quantity added from recognition")
	}

	if cacheReady && applied > 0 {
		if err := persistItemsPreserveSync(persistItems, lastSync, hasData); err != nil {
			log.Error().
				Err(err).
				Str("component", componentAddItemData).
				Msg("failed to persist item quantities")
			return false
		}
	}

	log.Info().
		Str("component", componentAddItemData).
		Int("hit_count", applied).
		Int("added_total", addedTotal).
		Bool("cache_ready", cacheReady).
		Str("grid_type", gridType).
		Strs("item_filters", params.ItemFilters).
		Msg("add item data finished")
	return true
}

func parseAddItemDataParam(raw string) (addItemDataParam, error) {
	var params addItemDataParam
	if strings.TrimSpace(raw) == "" {
		return params, nil
	}
	if err := json.Unmarshal([]byte(raw), &params); err != nil {
		return addItemDataParam{}, err
	}
	params.GridType = strings.TrimSpace(params.GridType)
	filters, err := iconqty.NormalizeStringList(params.ItemFilters, "item_filters")
	if err != nil {
		return addItemDataParam{}, err
	}
	params.ItemFilters = filters
	return params, nil
}

// resetCursorBeforeRecognition runs IMSA3MouseMoveReset before A3 recognition
// so the cursor does not occlude reward icons. Controller-specific behavior is
// owned by Pipeline overlays (e.g. ADB DoNothing). Screencap is refreshed
// afterwards so CacheImage reflects any cursor change.
func resetCursorBeforeRecognition(ctx *maa.Context, ctrl *maa.Controller) {
	if ctx == nil || ctrl == nil {
		return
	}
	if _, err := ctx.RunAction(nodeIMSA3MouseMoveReset, maa.Rect{0, 0, 0, 0}, "", nil); err != nil {
		log.Warn().
			Err(err).
			Str("component", componentAddItemData).
			Str("node", nodeIMSA3MouseMoveReset).
			Msg("failed to run IMSA3MouseMoveReset before recognition")
		return
	}
	ctrl.PostScreencap().Wait()
	log.Info().
		Str("component", componentAddItemData).
		Str("node", nodeIMSA3MouseMoveReset).
		Msg("ran IMSA3MouseMoveReset before recognition")
}
