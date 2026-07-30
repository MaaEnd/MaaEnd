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
// page_dedup: 翻页去重。false=本轮结果整表创建；true=在已有缓存上按 ID 覆盖数量。
type syncItemDataParam struct {
	Items     map[string]string `json:"items"`
	PageDedup bool              `json:"page_dedup"`
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

	merged, err := baseItemsForSync(params.PageDedup)
	if err != nil {
		log.Error().
			Err(err).
			Str("component", componentSyncItemData).
			Msg("failed to prepare base items")
		return false
	}

	itemIDs := make([]string, 0, len(params.Items))
	for itemID := range params.Items {
		itemIDs = append(itemIDs, itemID)
	}
	sort.Strings(itemIDs)

	hitCount := 0
	for _, itemID := range itemIDs {
		nodeName := strings.TrimSpace(params.Items[itemID])
		itemID = strings.TrimSpace(itemID)
		if itemID == "" || nodeName == "" {
			log.Error().
				Str("component", componentSyncItemData).
				Str("item_id", itemID).
				Str("node", nodeName).
				Msg("items contains empty item id or node name")
			return false
		}

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
		maafocus.Print(ctx, i18n.T("ims.sync_item_found", displayName, qty))
		log.Info().
			Str("component", componentSyncItemData).
			Str("item_id", itemID).
			Str("item_name", displayName).
			Str("node", nodeName).
			Int("quantity", qty).
			Int("previous", prev).
			Bool("overwrote", existed).
			Bool("page_dedup", params.PageDedup).
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
		Int("item_param_count", len(params.Items)).
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
	return params, nil
}

func baseItemsForSync(pageDedup bool) (map[string]int, error) {
	if !pageDedup {
		return map[string]int{}, nil
	}
	// Caller must ensureHydrated first; memory is the session source of truth.
	return ItemsSnapshot(), nil
}

func recognizeItemQuantity(ctx *maa.Context, andNode string, img image.Image) (qty int, hit bool, err error) {
	detail, err := ctx.RunRecognition(andNode, img)
	if err != nil {
		return 0, false, fmt.Errorf("run recognition %s: %w", andNode, err)
	}
	if detail == nil || !detail.Hit {
		return 0, false, nil
	}

	selected, err := recogtarget.SelectDetail(ctx, andNode, detail)
	if err != nil {
		return 0, false, fmt.Errorf("select box_index detail: %w", err)
	}
	qty, err = extractOCRQuantity(selected)
	if err != nil {
		return 0, false, fmt.Errorf("parse quantity from %s: %w", andNode, err)
	}
	return qty, true, nil
}

func itemDisplayName(itemID string) string {
	key := "ims.item." + itemID
	name := i18n.T(key)
	if name == key || strings.TrimSpace(name) == "" {
		return itemID
	}
	return name
}
