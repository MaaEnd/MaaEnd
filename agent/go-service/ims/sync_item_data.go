package ims

import (
	"encoding/json"
	"fmt"
	"image"
	"sort"
	"strings"
	"sync"
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

const (
	syncMergeModeReplace = "replace"
	syncMergeModeSum     = "sum"

	syncTransactionModeBegin    = "begin"
	syncTransactionModeContinue = "continue"
	syncTransactionModeCommit   = "commit"
)

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
// At least one of grid_type (icon scan) / items must be set, except for a
// transaction_mode=commit call that only publishes existing staging.
// page_dedup=false region rebuild = IconRecognition catalog IDs from item_filters
// (when grid_type is set) UNION keys of items.
// merge_mode=sum captures an immutable baseline on page_dedup=false, then
// writes baseline + recognized absolute quantity on every page. This makes a
// second inventory region idempotent even when adjacent pages overlap.
// transaction_mode=begin/continue stages pages for the current TaskID without
// updating the public cache; transaction_mode=commit persists that completed
// staging snapshot atomically from the Pipeline's point of view.
type syncItemDataParam struct {
	GridType        string            `json:"grid_type"`
	ROI             []int             `json:"roi"`
	ItemFilters     []string          `json:"item_filters"`
	Items           map[string]string `json:"items"`
	PageDedup       bool              `json:"page_dedup"`
	NotifyUI        *bool             `json:"notify_ui"`
	Deduplicate     *bool             `json:"deduplicate"`
	MergeMode       string            `json:"merge_mode"`
	TransactionMode string            `json:"transaction_mode"`
}

// SyncItemData scans configured items or commits a staged multi-page snapshot.
type SyncItemData struct {
	sumMu     sync.Mutex
	sumTaskID int64
	sumBase   map[string]int

	transactionMu      sync.Mutex
	transactionTaskID  int64
	transactionItems   map[string]int
	transactionSumBase map[string]int
	transactionHitIDs  map[string]struct{}
}

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

	notifyUI := resolveSyncNotifyUI(params.NotifyUI)
	if params.TransactionMode == syncTransactionModeCommit {
		if err := ensureHydrated(); err != nil {
			log.Error().
				Err(err).
				Str("component", componentSyncItemData).
				Msg("failed to hydrate ims cache")
			return false
		}
		return a.commitTransactionWithReport(ctx, arg.TaskID, notifyUI)
	}

	wantsIcon := wantsIconScan(params)
	if !wantsIcon && len(params.Items) == 0 {
		log.Error().
			Str("component", componentSyncItemData).
			Msg("grid_type or items must not be empty")
		return false
	}

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
	if wantsIcon && (!params.PageDedup || params.MergeMode == syncMergeModeSum) {
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
	merged, sumBase, err := a.prepareSyncBase(arg.TaskID, params, scanIDs)
	if err != nil {
		log.Error().
			Err(err).
			Str("component", componentSyncItemData).
			Str("merge_mode", params.MergeMode).
			Bool("page_dedup", params.PageDedup).
			Msg("failed to prepare base items")
		return false
	}

	hitCount := 0
	hitItemIDs := make([]string, 0)
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
			quantity := mergedSyncQuantity(params.MergeMode, sumBase, h.ItemID, h.Qty)
			merged[h.ItemID] = quantity
			hitCount++
			hitItemIDs = append(hitItemIDs, h.ItemID)
			displayName := iconqty.ItemDisplayName(h.ItemID)
			if notifyUI {
				maafocus.Print(ctx, i18n.T("ims.sync_item_found", displayName, quantity))
			}
			log.Info().
				Str("component", componentSyncItemData).
				Str("item_id", h.ItemID).
				Str("item_name", displayName).
				Str("source", "IconRecognition").
				Int("recognized_quantity", h.Qty).
				Int("quantity", quantity).
				Int("previous", prev).
				Bool("overwrote", existed).
				Bool("page_dedup", params.PageDedup).
				Str("merge_mode", params.MergeMode).
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
		quantity := mergedSyncQuantity(params.MergeMode, sumBase, itemID, qty)
		merged[itemID] = quantity
		hitCount++
		hitItemIDs = append(hitItemIDs, itemID)
		displayName := iconqty.ItemDisplayName(itemID)
		if notifyUI {
			maafocus.Print(ctx, i18n.T("ims.sync_item_found", displayName, quantity))
		}
		log.Info().
			Str("component", componentSyncItemData).
			Str("item_id", itemID).
			Str("item_name", displayName).
			Str("node", nodeName).
			Str("source", "pipeline_ocr").
			Int("recognized_quantity", qty).
			Int("quantity", quantity).
			Int("previous", prev).
			Bool("overwrote", existed).
			Bool("page_dedup", params.PageDedup).
			Str("merge_mode", params.MergeMode).
			Bool("notify_ui", notifyUI).
			Msg("item quantity recorded")
	}

	persisted := params.TransactionMode == ""
	var at time.Time
	if persisted {
		at = time.Now()
		if err := persistSynced(at, merged); err != nil {
			log.Error().
				Err(err).
				Str("component", componentSyncItemData).
				Msg("failed to persist ims record")
			return false
		}
	} else if err := a.storeTransactionResult(arg.TaskID, merged, hitItemIDs...); err != nil {
		log.Error().
			Err(err).
			Str("component", componentSyncItemData).
			Str("transaction_mode", params.TransactionMode).
			Msg("failed to stage ims record")
		return false
	}

	event := log.Info().
		Str("component", componentSyncItemData).
		Str("grid_type", params.GridType).
		Strs("item_filters", params.ItemFilters).
		Int("region_candidate_ids", len(regionIDs)).
		Int("ocr_item_count", len(params.Items)).
		Int("hit_count", hitCount).
		Int("total_cached", len(merged)).
		Bool("page_dedup", params.PageDedup).
		Str("merge_mode", params.MergeMode).
		Str("transaction_mode", params.TransactionMode).
		Bool("persisted", persisted)
	if persisted {
		event = event.Time("updated_at", at.UTC())
	}
	event.Msg("item data sync finished")
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
	params.MergeMode = strings.TrimSpace(params.MergeMode)
	params.TransactionMode = strings.TrimSpace(params.TransactionMode)
	if params.MergeMode == "" {
		params.MergeMode = syncMergeModeReplace
	}
	if params.MergeMode != syncMergeModeReplace && params.MergeMode != syncMergeModeSum {
		return syncItemDataParam{}, fmt.Errorf("unsupported merge_mode %q", params.MergeMode)
	}
	if params.TransactionMode != "" &&
		params.TransactionMode != syncTransactionModeBegin &&
		params.TransactionMode != syncTransactionModeContinue &&
		params.TransactionMode != syncTransactionModeCommit {
		return syncItemDataParam{}, fmt.Errorf("unsupported transaction_mode %q", params.TransactionMode)
	}
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

// prepareSyncBase returns the cache copy to update and, for sum mode, the
// immutable first-depot baseline used to make overlapping pages idempotent.
func (a *SyncItemData) prepareSyncBase(
	taskID int64,
	params syncItemDataParam,
	scanItemIDs []string,
) (merged, sumBase map[string]int, err error) {
	if params.TransactionMode != "" {
		if params.TransactionMode == syncTransactionModeBegin {
			a.clearSumSession()
		}
		return a.prepareTransactionBase(taskID, params, scanItemIDs)
	}

	if params.MergeMode != syncMergeModeSum {
		if !params.PageDedup {
			a.clearSumSession()
		}
		merged, err = baseItemsForSync(params.PageDedup, scanItemIDs)
		return merged, nil, err
	}

	a.sumMu.Lock()
	defer a.sumMu.Unlock()

	if !params.PageDedup {
		snapshot := ItemsSnapshot()
		a.sumTaskID = taskID
		a.sumBase = make(map[string]int, len(scanItemIDs))
		for _, id := range scanItemIDs {
			a.sumBase[id] = snapshot[id]
		}
		return snapshot, copyItemQuantities(a.sumBase), nil
	}

	if a.sumBase == nil {
		return nil, nil, fmt.Errorf("sum continuation has no active baseline")
	}
	if a.sumTaskID != taskID {
		return nil, nil, fmt.Errorf("sum continuation task_id=%d does not match baseline task_id=%d", taskID, a.sumTaskID)
	}
	return ItemsSnapshot(), copyItemQuantities(a.sumBase), nil
}

// prepareTransactionBase returns an isolated staging copy for the current
// TaskID. The formal IMS cache is never used as continuation state after begin.
func (a *SyncItemData) prepareTransactionBase(
	taskID int64,
	params syncItemDataParam,
	scanItemIDs []string,
) (merged, sumBase map[string]int, err error) {
	if params.TransactionMode != syncTransactionModeBegin &&
		params.TransactionMode != syncTransactionModeContinue {
		return nil, nil, fmt.Errorf("transaction_mode %q cannot scan items", params.TransactionMode)
	}

	a.transactionMu.Lock()
	defer a.transactionMu.Unlock()

	if params.TransactionMode == syncTransactionModeBegin {
		merged, err = baseItemsForSync(params.PageDedup, scanItemIDs)
		if err != nil {
			return nil, nil, err
		}
		a.transactionTaskID = taskID
		a.transactionItems = copyItemQuantities(merged)
		a.transactionSumBase = nil
		a.transactionHitIDs = make(map[string]struct{})
	} else {
		if a.transactionItems == nil {
			return nil, nil, fmt.Errorf("transaction continuation has no active staging snapshot")
		}
		if a.transactionTaskID != taskID {
			return nil, nil, fmt.Errorf(
				"transaction continuation task_id=%d does not match staging task_id=%d",
				taskID,
				a.transactionTaskID,
			)
		}
		merged = copyItemQuantities(a.transactionItems)
	}

	if params.MergeMode != syncMergeModeSum {
		if !params.PageDedup && params.TransactionMode == syncTransactionModeContinue {
			for _, id := range scanItemIDs {
				delete(merged, id)
			}
		}
		return merged, nil, nil
	}

	if !params.PageDedup {
		a.transactionSumBase = make(map[string]int, len(scanItemIDs))
		for _, id := range scanItemIDs {
			a.transactionSumBase[id] = merged[id]
		}
		return merged, copyItemQuantities(a.transactionSumBase), nil
	}
	if a.transactionSumBase == nil {
		return nil, nil, fmt.Errorf("sum continuation has no active transaction baseline")
	}
	return merged, copyItemQuantities(a.transactionSumBase), nil
}

func (a *SyncItemData) storeTransactionResult(taskID int64, items map[string]int, hitItemIDs ...string) error {
	a.transactionMu.Lock()
	defer a.transactionMu.Unlock()
	if a.transactionItems == nil {
		return fmt.Errorf("transaction has no active staging snapshot")
	}
	if a.transactionTaskID != taskID {
		return fmt.Errorf(
			"transaction task_id=%d does not match staging task_id=%d",
			taskID,
			a.transactionTaskID,
		)
	}
	a.transactionItems = copyItemQuantities(items)
	if a.transactionHitIDs == nil {
		a.transactionHitIDs = make(map[string]struct{})
	}
	for _, itemID := range hitItemIDs {
		itemID = strings.TrimSpace(itemID)
		if itemID != "" {
			a.transactionHitIDs[itemID] = struct{}{}
		}
	}
	return nil
}

func (a *SyncItemData) commitTransaction(taskID int64) bool {
	return a.commitTransactionWithReport(nil, taskID, false)
}

func (a *SyncItemData) commitTransactionWithReport(ctx *maa.Context, taskID int64, notifyUI bool) bool {
	a.transactionMu.Lock()
	if a.transactionItems == nil {
		a.transactionMu.Unlock()
		log.Error().
			Str("component", componentSyncItemData).
			Int64("task_id", taskID).
			Msg("transaction commit has no active staging snapshot")
		return false
	}
	if a.transactionTaskID != taskID {
		stagingTaskID := a.transactionTaskID
		a.transactionMu.Unlock()
		log.Error().
			Str("component", componentSyncItemData).
			Int64("task_id", taskID).
			Int64("staging_task_id", stagingTaskID).
			Msg("transaction commit task does not match staging task")
		return false
	}

	at := time.Now()
	if err := persistSynced(at, a.transactionItems); err != nil {
		a.transactionMu.Unlock()
		log.Error().
			Err(err).
			Str("component", componentSyncItemData).
			Int64("task_id", taskID).
			Msg("failed to commit staged ims record")
		return false
	}

	totalCached := len(a.transactionItems)
	reportItems := make(map[string]int, len(a.transactionHitIDs))
	for itemID := range a.transactionHitIDs {
		if quantity, ok := a.transactionItems[itemID]; ok {
			reportItems[itemID] = quantity
		}
	}
	a.transactionTaskID = 0
	a.transactionItems = nil
	a.transactionSumBase = nil
	a.transactionHitIDs = nil
	a.transactionMu.Unlock()

	reportedItemCount := 0
	if notifyUI && ctx != nil {
		reportedItemCount = reportSyncedItems(ctx, reportItems)
	}
	log.Info().
		Str("component", componentSyncItemData).
		Int64("task_id", taskID).
		Int("total_cached", totalCached).
		Bool("notify_ui", notifyUI).
		Int("reported_item_count", reportedItemCount).
		Time("updated_at", at.UTC()).
		Msg("staged item data committed")
	return true
}

type syncedItemReport struct {
	itemID   string
	name     string
	quantity int
}

func reportSyncedItems(ctx *maa.Context, items map[string]int) int {
	reports := make([]syncedItemReport, 0, len(items))
	for itemID, quantity := range items {
		reports = append(reports, syncedItemReport{
			itemID:   itemID,
			name:     iconqty.ItemDisplayName(itemID),
			quantity: quantity,
		})
	}
	sort.Slice(reports, func(i, j int) bool {
		if reports[i].name != reports[j].name {
			return reports[i].name < reports[j].name
		}
		return reports[i].itemID < reports[j].itemID
	})
	for _, report := range reports {
		maafocus.Print(ctx, i18n.T("ims.sync_item_found", report.name, report.quantity))
	}
	return len(reports)
}

func (a *SyncItemData) clearSumSession() {
	a.sumMu.Lock()
	defer a.sumMu.Unlock()
	a.sumTaskID = 0
	a.sumBase = nil
}

func copyItemQuantities(items map[string]int) map[string]int {
	out := make(map[string]int, len(items))
	for id, qty := range items {
		out[id] = qty
	}
	return out
}

func mergedSyncQuantity(mergeMode string, sumBase map[string]int, itemID string, recognized int) int {
	if mergeMode == syncMergeModeSum {
		return sumBase[itemID] + recognized
	}
	return recognized
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
