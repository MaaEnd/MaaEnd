package ims

import (
	"encoding/json"
	"fmt"
	"image"
	"sort"
	"strings"
	"time"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/iconqty"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/maafocus"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/ocrnum"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/recogtarget"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const componentSyncItemData = "SyncItemData"

var _ maa.CustomActionRunner = &SyncItemData{}

// syncItemDataParam is custom_action_param for SyncItemData (A2).
//
// IconRecognition path (贵重品库等网格界面):
//   - grid_type + optional item_filters / roi via pkg/iconqty;
//   - every returned match quantity is OCR'd from cell_box.
//
// OCR / And+box_index path (顶栏货币、采购中心定点数字等):
//   - items maps cache item ID -> pipeline recognition node name;
//   - the node may be pure OCR, or And whose box_index selects the OCR digit result;
//   - these keys always join region rebuild when page_dedup=false (miss → drop).
//
// At least one of grid_type (icon scan) / items must be set.
// page_dedup=false region rebuild = IconRecognition catalog IDs from item_filters
// (when grid_type is set) UNION keys of items.
type syncItemDataParam struct {
	GridType    string            `json:"grid_type"`
	ROI         []int             `json:"roi"`
	ItemFilters []string          `json:"item_filters"`
	Items       map[string]string `json:"items"`
	PageDedup   bool              `json:"page_dedup"`
	NotifyUI    *bool             `json:"notify_ui"`
	Deduplicate *bool             `json:"deduplicate"`
}

// SyncItemData scans configured items on the current screen and persists quantities.
type SyncItemData struct{}

// Run implements maa.CustomActionRunner.
func (a *SyncItemData) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if ctx == nil || arg == nil {
		log.Error().
			Str("component", componentSyncItemData).
			Msg("nil context or arg")
		return false
	}

	params, err := parseSyncItemDataParam(arg.CustomActionParam)
	if err != nil {
		log.Error().
			Err(err).
			Str("component", componentSyncItemData).
			Str("custom_action_param", arg.CustomActionParam).
			Msg("failed to parse params")
		return false
	}

	wantsIcon := wantsIconScan(params)
	if !wantsIcon && len(params.Items) == 0 {
		log.Error().
			Str("component", componentSyncItemData).
			Msg("grid_type or items must not be empty")
		return false
	}

	notifyUI := resolveSyncNotifyUI(params.NotifyUI)

	if err := ensureHydrated(); err != nil {
		log.Error().
			Err(err).
			Str("component", componentSyncItemData).
			Msg("failed to hydrate ims cache")
		return false
	}

	tasker := ctx.GetTasker()
	if tasker == nil || tasker.GetController() == nil {
		log.Error().
			Str("component", componentSyncItemData).
			Msg("tasker or controller is nil")
		return false
	}
	img, err := tasker.GetController().CacheImage()
	if err != nil || img == nil {
		log.Error().
			Err(err).
			Str("component", componentSyncItemData).
			Msg("failed to cache image")
		return false
	}

	var regionIDs []string
	if wantsIcon && !params.PageDedup {
		regionIDs, err = resolveRegionRebuildIDs(params.GridType, params.ItemFilters)
		if err != nil {
			log.Error().
				Err(err).
				Str("component", componentSyncItemData).
				Str("grid_type", params.GridType).
				Strs("item_filters", params.ItemFilters).
				Msg("failed to resolve region rebuild IDs from IconRecognition catalog")
			return false
		}
	}
	scanIDs := collectSyncScanIDs(regionIDs, params.Items)
	merged, err := baseItemsForSync(params.PageDedup, scanIDs)
	if err != nil {
		log.Error().
			Err(err).
			Str("component", componentSyncItemData).
			Msg("failed to prepare base items")
		return false
	}

	hitCount := 0
	if wantsIcon {
		dedup := true
		if params.Deduplicate != nil {
			dedup = *params.Deduplicate
		}
		hits, err := iconqty.RecognizeQuantities(ctx, img, iconqty.Request{
			GridType:    params.GridType,
			ROI:         params.ROI,
			ItemFilters: params.ItemFilters,
			Deduplicate: dedup,
		})
		if err != nil {
			log.Error().
				Err(err).
				Str("component", componentSyncItemData).
				Str("grid_type", params.GridType).
				Strs("item_filters", params.ItemFilters).
				Msg("failed to recognize icon items")
			return false
		}
		for _, h := range hits {
			prev, existed := merged[h.ItemID]
			merged[h.ItemID] = h.Qty
			hitCount++
			displayName := iconqty.ItemDisplayName(h.ItemID)
			if notifyUI {
				maafocus.Print(ctx, i18n.T("ims.sync_item_found", displayName, h.Qty))
			}
			log.Info().
				Str("component", componentSyncItemData).
				Str("item_id", h.ItemID).
				Str("item_name", displayName).
				Str("source", "IconRecognition").
				Int("quantity", h.Qty).
				Int("previous", prev).
				Bool("overwrote", existed).
				Bool("page_dedup", params.PageDedup).
				Bool("notify_ui", notifyUI).
				Msg("item quantity recorded")
		}
	}

	ocrItemIDs := make([]string, 0, len(params.Items))
	for itemID := range params.Items {
		ocrItemIDs = append(ocrItemIDs, itemID)
	}
	sort.Strings(ocrItemIDs)
	for _, itemID := range ocrItemIDs {
		nodeName := params.Items[itemID]
		qty, ok, err := recognizeItemQuantity(ctx, nodeName, img)
		if err != nil {
			log.Error().
				Err(err).
				Str("component", componentSyncItemData).
				Str("item_id", itemID).
				Str("node", nodeName).
				Msg("failed to recognize item")
			return false
		}
		if !ok {
			log.Info().
				Str("component", componentSyncItemData).
				Str("item_id", itemID).
				Str("node", nodeName).
				Bool("page_dedup", params.PageDedup).
				Msg("item recognizer not hit, skip")
			continue
		}

		prev, existed := merged[itemID]
		merged[itemID] = qty
		hitCount++
		displayName := iconqty.ItemDisplayName(itemID)
		if notifyUI {
			maafocus.Print(ctx, i18n.T("ims.sync_item_found", displayName, qty))
		}
		log.Info().
			Str("component", componentSyncItemData).
			Str("item_id", itemID).
			Str("item_name", displayName).
			Str("node", nodeName).
			Str("source", "pipeline_ocr").
			Int("quantity", qty).
			Int("previous", prev).
			Bool("overwrote", existed).
			Bool("page_dedup", params.PageDedup).
			Bool("notify_ui", notifyUI).
			Msg("item quantity recorded")
	}

	at := time.Now()
	if err := persistSynced(at, merged); err != nil {
		log.Error().
			Err(err).
			Str("component", componentSyncItemData).
			Msg("failed to persist ims record")
		return false
	}

	log.Info().
		Str("component", componentSyncItemData).
		Str("grid_type", params.GridType).
		Strs("item_filters", params.ItemFilters).
		Int("region_rebuild_ids", len(regionIDs)).
		Int("ocr_item_count", len(params.Items)).
		Int("hit_count", hitCount).
		Int("total_cached", len(merged)).
		Bool("page_dedup", params.PageDedup).
		Time("updated_at", at.UTC()).
		Msg("item data sync finished")
	return true
}

func wantsIconScan(params syncItemDataParam) bool {
	return strings.TrimSpace(params.GridType) != ""
}

func collectSyncScanIDs(regionIDs []string, items map[string]string) []string {
	seen := make(map[string]struct{}, len(regionIDs)+len(items))
	out := make([]string, 0, len(regionIDs)+len(items))
	for _, id := range regionIDs {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	for id := range items {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

func parseSyncItemDataParam(raw string) (syncItemDataParam, error) {
	var params syncItemDataParam
	if strings.TrimSpace(raw) == "" {
		return params, fmt.Errorf("custom_action_param is empty")
	}
	if err := json.Unmarshal([]byte(raw), &params); err != nil {
		return syncItemDataParam{}, err
	}
	normalizedItems, err := normalizeItemsMap(params.Items)
	if err != nil {
		return syncItemDataParam{}, err
	}
	params.Items = normalizedItems
	params.ItemFilters, err = iconqty.NormalizeStringList(params.ItemFilters, "item_filters")
	if err != nil {
		return syncItemDataParam{}, err
	}
	params.GridType = strings.TrimSpace(params.GridType)
	return params, nil
}

// normalizeItemsMap trims item IDs and node names so cache keys stay consistent
// across region rebuild, recognition, and persistence.
func normalizeItemsMap(items map[string]string) (map[string]string, error) {
	if len(items) == 0 {
		return items, nil
	}
	out := make(map[string]string, len(items))
	for id, node := range items {
		id = strings.TrimSpace(id)
		node = strings.TrimSpace(node)
		if id == "" || node == "" {
			return nil, fmt.Errorf("items contains empty item id or node name")
		}
		if _, dup := out[id]; dup {
			return nil, fmt.Errorf("items contains duplicate item id after trim: %s", id)
		}
		out[id] = node
	}
	return out, nil
}

// resolveSyncNotifyUI defaults to true when omitted (announce each hit item).
func resolveSyncNotifyUI(v *bool) bool {
	if v == nil {
		return true
	}
	return *v
}

func baseItemsForSync(pageDedup bool, scanItemIDs []string) (map[string]int, error) {
	// Caller must ensureHydrated first; memory is the session source of truth.
	snap := ItemsSnapshot()
	if pageDedup {
		return snap, nil
	}
	// Region rebuild: drop only IDs belonging to this scan, keep other regions.
	out := make(map[string]int, len(snap))
	for id, qty := range snap {
		out[id] = qty
	}
	for _, id := range scanItemIDs {
		delete(out, id)
	}
	return out, nil
}

func recognizeItemQuantity(ctx *maa.Context, andNode string, img image.Image) (qty int, hit bool, err error) {
	qty, hit, _, err = recognizeItemQuantityHit(ctx, andNode, img)
	return qty, hit, err
}

// recognizeItemQuantityHit runs a pipeline recognition node and returns quantity
// plus the root recognition detail (OCR / And+box_index path).
func recognizeItemQuantityHit(
	ctx *maa.Context,
	andNode string,
	img image.Image,
) (qty int, hit bool, detail *maa.RecognitionDetail, err error) {
	detail, err = ctx.RunRecognition(andNode, img)
	if err != nil {
		return 0, false, nil, fmt.Errorf("run recognition %s: %w", andNode, err)
	}
	if detail == nil || !detail.Hit {
		return 0, false, detail, nil
	}

	selected, err := recogtarget.SelectDetail(ctx, andNode, detail)
	if err != nil {
		return 0, false, detail, fmt.Errorf("select box_index detail: %w", err)
	}
	qty, err = ocrnum.Extract(selected)
	if err != nil {
		return 0, false, detail, fmt.Errorf("parse quantity from %s: %w", andNode, err)
	}
	return qty, true, detail, nil
}
