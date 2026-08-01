package ims

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/control"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/maafocus"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const componentAddItemData = "AddItemData"

var _ maa.CustomActionRunner = &AddItemData{}

// addItemDataParam is custom_action_param for AddItemData (A3).
//
// items: 字典，键为物品 ID，值为识别节点名；依次识别，将 OCR 数量作为正增量写入缓存。
type addItemDataParam struct {
	Items map[string]string `json:"items"`
}

// AddItemData recognizes configured items on the current screen and adds their
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

type recognizedItemAdd struct {
	itemID string
	node   string
	qty    int
}

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
	if len(params.Items) == 0 {
		log.Error().
			Str("component", componentAddItemData).
			Msg("items must not be empty")
		return false
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
	moveCursorTopLeftIfWin32(ctrl)
	img, err := ctrl.CacheImage()
	if err != nil || img == nil {
		log.Error().
			Err(err).
			Str("component", componentAddItemData).
			Msg("failed to cache image")
		return false
	}

	itemIDs := make([]string, 0, len(params.Items))
	for itemID := range params.Items {
		itemIDs = append(itemIDs, itemID)
	}
	sort.Strings(itemIDs)

	hits := make([]recognizedItemAdd, 0, len(itemIDs))
	for _, itemID := range itemIDs {
		nodeName := strings.TrimSpace(params.Items[itemID])
		itemID = strings.TrimSpace(itemID)
		if itemID == "" || nodeName == "" {
			log.Error().
				Str("component", componentAddItemData).
				Str("item_id", itemID).
				Str("node", nodeName).
				Msg("items contains empty item id or node name")
			return false
		}

		qty, ok, err := recognizeItemQuantity(ctx, nodeName, img)
		if err != nil {
			log.Error().
				Err(err).
				Str("component", componentAddItemData).
				Str("item_id", itemID).
				Str("node", nodeName).
				Msg("failed to recognize item")
			return false
		}
		if !ok {
			log.Info().
				Str("component", componentAddItemData).
				Str("item_id", itemID).
				Str("node", nodeName).
				Msg("item recognizer not hit, skip")
			continue
		}
		if qty <= 0 {
			log.Info().
				Str("component", componentAddItemData).
				Str("item_id", itemID).
				Str("node", nodeName).
				Int("quantity", qty).
				Msg("non-positive quantity, skip")
			continue
		}
		hits = append(hits, recognizedItemAdd{itemID: itemID, node: nodeName, qty: qty})
	}

	addedTotal := 0
	var (
		persistItems map[string]int
		lastSync     time.Time
		hasData      bool
	)
	for _, h := range hits {
		displayName := itemDisplayName(h.itemID)
		maafocus.Print(ctx, i18n.T("ims.add_item_found", displayName, h.qty))
		addedTotal += h.qty

		if !cacheReady {
			log.Info().
				Str("component", componentAddItemData).
				Str("item_id", h.itemID).
				Str("item_name", displayName).
				Str("node", h.node).
				Int("delta", h.qty).
				Bool("cache_ready", false).
				Msg("item recognized, skip cache write")
			continue
		}

		before, after, _, items, syncAt, ready := globalCache.applyDelta(h.itemID, h.qty)
		persistItems = items
		lastSync = syncAt
		hasData = ready
		log.Info().
			Str("component", componentAddItemData).
			Str("item_id", h.itemID).
			Str("item_name", displayName).
			Str("node", h.node).
			Int("delta", h.qty).
			Int("before", before).
			Int("after", after).
			Msg("item quantity added from recognition")
	}

	if cacheReady && len(hits) > 0 {
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
		Int("item_param_count", len(params.Items)).
		Int("hit_count", len(hits)).
		Int("added_total", addedTotal).
		Bool("cache_ready", cacheReady).
		Msg("add item data finished")
	return true
}

func parseAddItemDataParam(raw string) (addItemDataParam, error) {
	var params addItemDataParam
	if strings.TrimSpace(raw) == "" {
		return params, fmt.Errorf("custom_action_param is empty")
	}
	if err := json.Unmarshal([]byte(raw), &params); err != nil {
		return addItemDataParam{}, err
	}
	return params, nil
}

// moveCursorTopLeftIfWin32 moves the cursor to the top-left corner before A3
// recognition so it does not occlude reward icons. Win32 only; pressure 0 means
// move-only (no click). Non-Win32 controllers are skipped.
func moveCursorTopLeftIfWin32(ctrl *maa.Controller) {
	if ctrl == nil {
		return
	}
	controlType, err := control.GetControlType(ctrl)
	if err != nil {
		log.Warn().
			Err(err).
			Str("component", componentAddItemData).
			Msg("failed to resolve controller type, skip cursor move")
		return
	}
	if controlType != control.CONTROL_TYPE_WIN32 {
		return
	}
	ctrl.PostTouchMove(0, 0, 0, 0).Wait()
	ctrl.PostScreencap().Wait()
	log.Info().
		Str("component", componentAddItemData).
		Str("controller_type", controlType).
		Int("x", 0).
		Int("y", 0).
		Msg("moved cursor to top-left before recognition")
}
