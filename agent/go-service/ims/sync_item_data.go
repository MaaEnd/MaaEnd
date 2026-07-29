package ims

import (
	"encoding/json"
	"fmt"
	"image"
	"strings"
	"time"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/recogtarget"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const componentSyncItemData = "SyncItemData"

var _ maa.CustomActionRunner = &SyncItemData{}

// syncItemDataParam is custom_action_param for SyncItemData.
//
// items: And 识别节点名列表，依次执行；box_index 指向的数量子节点名即物品 ID。
// page_dedup: 翻页去重。false=本轮结果整表创建；true=在已有缓存上按 ID 覆盖数量。
type syncItemDataParam struct {
	Items     []string `json:"items"`
	PageDedup bool     `json:"page_dedup"`
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

	hitCount := 0
	for _, nodeName := range params.Items {
		nodeName = strings.TrimSpace(nodeName)
		if nodeName == "" {
			log.Error().
				Str("component", componentSyncItemData).
				Msg("items contains empty node name")
			return false
		}

		itemID, qty, ok, err := recognizeItemQuantity(ctx, nodeName, img)
		if err != nil {
			log.Error().
				Err(err).
				Str("component", componentSyncItemData).
				Str("node", nodeName).
				Msg("failed to recognize item")
			return false
		}
		if !ok {
			log.Info().
				Str("component", componentSyncItemData).
				Str("node", nodeName).
				Bool("page_dedup", params.PageDedup).
				Msg("item recognizer not hit, skip")
			continue
		}

		prev, existed := merged[itemID]
		merged[itemID] = qty
		hitCount++
		log.Info().
			Str("component", componentSyncItemData).
			Str("node", nodeName).
			Str("item_id", itemID).
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
	items := ItemsSnapshot()
	if len(items) > 0 {
		return items, nil
	}
	rec, err := loadRecord()
	if err != nil {
		return nil, err
	}
	return rec.Items, nil
}

func recognizeItemQuantity(ctx *maa.Context, andNode string, img image.Image) (itemID string, qty int, hit bool, err error) {
	itemID, err = resolveQuantityNodeName(ctx, andNode)
	if err != nil {
		return "", 0, false, err
	}

	detail, err := ctx.RunRecognition(andNode, img)
	if err != nil {
		return "", 0, false, fmt.Errorf("run recognition %s: %w", andNode, err)
	}
	if detail == nil || !detail.Hit {
		return itemID, 0, false, nil
	}

	selected, err := recogtarget.SelectDetail(ctx, andNode, detail)
	if err != nil {
		return "", 0, false, fmt.Errorf("select box_index detail: %w", err)
	}
	qty, err = extractOCRQuantity(selected)
	if err != nil {
		return "", 0, false, fmt.Errorf("parse quantity for %s: %w", itemID, err)
	}
	return itemID, qty, true, nil
}

// resolveQuantityNodeName returns the And.box_index child node name used as item ID.
func resolveQuantityNodeName(ctx *maa.Context, andNode string) (string, error) {
	raw, err := ctx.GetNodeJSON(andNode)
	if err != nil {
		return "", fmt.Errorf("get node %s json: %w", andNode, err)
	}
	return resolveQuantityNodeNameFromJSON(andNode, []byte(raw))
}

func resolveQuantityNodeNameFromJSON(andNode string, raw []byte) (string, error) {
	fields, err := recogtarget.ParseNodeJSON(raw)
	if err != nil {
		return "", fmt.Errorf("parse node %s: %w", andNode, err)
	}
	if fields.Type != "And" {
		return "", fmt.Errorf("node %s must be And, got %s", andNode, fields.Type)
	}
	if len(fields.AllOf) == 0 {
		return "", fmt.Errorf("node %s all_of is empty", andNode)
	}
	if fields.BoxIndex < 0 || fields.BoxIndex >= len(fields.AllOf) {
		return "", fmt.Errorf("node %s box_index %d out of range", andNode, fields.BoxIndex)
	}
	child := fields.AllOf[fields.BoxIndex]
	var name string
	if err := json.Unmarshal(child, &name); err != nil {
		return "", fmt.Errorf("node %s box_index target must be a named node reference", andNode)
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("node %s box_index target name is empty", andNode)
	}
	return name, nil
}
