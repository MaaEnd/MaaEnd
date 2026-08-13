package ims

import (
	"encoding/json"
	"fmt"
	"image"
	"sort"
	"strings"
	"time"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/maafocus"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/recogtarget"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const componentSyncItemData = "SyncItemData"

var _ maa.CustomActionRunner = &SyncItemData{}

// syncItemDataParam is custom_action_param for SyncItemData.
//
// items: 字典，键为物品 ID，值为 And 识别节点名；依次执行节点，沿 box_index 链取 OCR 数量。
// A2 必须显式传入 items（含定点 OCR 如 T_CREDS_NUMBER / OROBERYL_NUMBER），不使用 items.json 默认清单。
// page_dedup: 翻页去重 / 地区重建。
//
//	false=仅重建本轮 items 内的 ID（未命中则从缓存删除这些 ID），其他地区已缓存 ID 保留；
//	true=在已有缓存上按命中 ID 覆盖数量，未命中保留旧值。
//
// notify_ui:
//   - omitted → default true（命中物品时 Focus 播报「物品名：数量」）
//   - false → 不播报物品命中（万能跳转顺手缓存等场景）
type syncItemDataParam struct {
	Items     map[string]string `json:"items"`
	PageDedup bool              `json:"page_dedup"`
	NotifyUI  *bool             `json:"notify_ui"`
}

// SyncItemData scans configured item recognizers on the current screen and persists quantities.
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
	if len(params.Items) == 0 {
		log.Error().
			Str("component", componentSyncItemData).
			Msg("items must not be empty")
		return false
	}
	items := params.Items
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

	itemIDs := make([]string, 0, len(items))
	for itemID := range items {
		itemIDs = append(itemIDs, itemID)
	}
	sort.Strings(itemIDs)

	merged, err := baseItemsForSync(params.PageDedup, itemIDs)
	if err != nil {
		log.Error().
			Err(err).
			Str("component", componentSyncItemData).
			Msg("failed to prepare base items")
		return false
	}

	hitCount := 0
	for _, itemID := range itemIDs {
		nodeName := items[itemID]
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
		displayName := itemDisplayName(itemID)
		if notifyUI {
			maafocus.Print(ctx, i18n.T("ims.sync_item_found", displayName, qty))
		}
		log.Info().
			Str("component", componentSyncItemData).
			Str("item_id", itemID).
			Str("item_name", displayName).
			Str("node", nodeName).
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
		Int("item_param_count", len(items)).
		Int("hit_count", hitCount).
		Int("total_cached", len(merged)).
		Bool("page_dedup", params.PageDedup).
		Time("updated_at", at.UTC()).
		Msg("item data sync finished")
	return true
}

func parseSyncItemDataParam(raw string) (syncItemDataParam, error) {
	var params syncItemDataParam
	if strings.TrimSpace(raw) == "" {
		return params, fmt.Errorf("custom_action_param is empty")
	}
	if err := json.Unmarshal([]byte(raw), &params); err != nil {
		return syncItemDataParam{}, err
	}
	normalized, err := normalizeItemsMap(params.Items)
	if err != nil {
		return syncItemDataParam{}, err
	}
	params.Items = normalized
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
	// scanItemIDs must already be normalized (see normalizeItemsMap).
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

// recognizeItemQuantityHit runs the item And node and returns quantity plus the
// root recognition detail so A3 can mask the matched TemplateMatch best box.
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
	qty, err = extractOCRQuantity(selected)
	if err != nil {
		return 0, false, detail, fmt.Errorf("parse quantity from %s: %w", andNode, err)
	}
	return qty, true, detail, nil
}

func itemDisplayName(itemID string) string {
	key := "ims.item." + itemID
	name := i18n.T(key)
	if name == key || strings.TrimSpace(name) == "" {
		return itemID
	}
	return name
}
