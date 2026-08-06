package ims

import (
	"encoding/json"
	"image"
	"image/color"
	"image/draw"
	"sort"
	"strings"
	"time"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/control"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/maafocus"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/minicv"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const (
	componentAddItemData = "AddItemData"
	// Safety cap when mask_hit_region keeps re-scanning the same item ID.
	addItemDataMaxHitsPerItem = 32
)

var (
	_ maa.CustomActionRunner = &AddItemData{}

	imsGreenMaskColor = color.RGBA{R: 0, G: 255, B: 0, A: 255}
)

// addItemDataParam is custom_action_param for AddItemData (A3).
//
// items: 字典，键为物品 ID，值为识别节点名；依次识别，将 OCR 数量作为正增量写入缓存。
// 省略或为空时，使用 assets/data/IMS/items.json 的 a3 全量清单。
// mask_hit_region: 命中后将该物品区域涂绿，并对同一物品继续识别直到未命中（仅 A3）。
// 省略时默认为 true。
type addItemDataParam struct {
	Items         map[string]string `json:"items"`
	MaskHitRegion *bool             `json:"mask_hit_region"`
}

func (p addItemDataParam) maskHitRegionEnabled() bool {
	if p.MaskHitRegion == nil {
		return true
	}
	return *p.MaskHitRegion
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
	items, err := resolveA3ItemsMap(params.Items)
	if err != nil {
		log.Error().
			Err(err).
			Str("component", componentAddItemData).
			Msg("failed to resolve items")
		return false
	}
	maskHitRegion := params.maskHitRegionEnabled()

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

	workImg := workingRGBA(img)
	if workImg == nil {
		log.Error().
			Str("component", componentAddItemData).
			Msg("failed to prepare working image")
		return false
	}

	itemIDs := make([]string, 0, len(items))
	for itemID := range items {
		itemIDs = append(itemIDs, itemID)
	}
	sort.Strings(itemIDs)

	hits := make([]recognizedItemAdd, 0, len(itemIDs))
	for _, itemID := range itemIDs {
		nodeName := strings.TrimSpace(items[itemID])
		itemID = strings.TrimSpace(itemID)
		if itemID == "" || nodeName == "" {
			log.Error().
				Str("component", componentAddItemData).
				Str("item_id", itemID).
				Str("node", nodeName).
				Msg("items contains empty item id or node name")
			return false
		}

		for hitIndex := 0; hitIndex < addItemDataMaxHitsPerItem; hitIndex++ {
			qty, ok, detail, err := recognizeItemQuantityHit(ctx, nodeName, workImg)
			if err != nil {
				log.Error().
					Err(err).
					Str("component", componentAddItemData).
					Str("item_id", itemID).
					Str("node", nodeName).
					Int("hit_index", hitIndex).
					Msg("failed to recognize item")
				return false
			}
			if !ok {
				if hitIndex == 0 {
					log.Info().
						Str("component", componentAddItemData).
						Str("item_id", itemID).
						Str("node", nodeName).
						Msg("item recognizer not hit, skip")
				}
				break
			}
			if qty <= 0 {
				log.Info().
					Str("component", componentAddItemData).
					Str("item_id", itemID).
					Str("node", nodeName).
					Int("quantity", qty).
					Int("hit_index", hitIndex).
					Msg("non-positive quantity, skip")
				if !maskHitRegion {
					break
				}
				// Still mask so a bad OCR box does not block other stacks.
				if masked := paintItemHitRegion(workImg, detail); masked {
					log.Info().
						Str("component", componentAddItemData).
						Str("item_id", itemID).
						Str("node", nodeName).
						Int("hit_index", hitIndex).
						Msg("masked non-positive hit region")
				}
				continue
			}
			hits = append(hits, recognizedItemAdd{itemID: itemID, node: nodeName, qty: qty})

			if !maskHitRegion {
				break
			}
			if !paintItemHitRegion(workImg, detail) {
				log.Warn().
					Str("component", componentAddItemData).
					Str("item_id", itemID).
					Str("node", nodeName).
					Int("hit_index", hitIndex).
					Msg("failed to mask hit region, stop rescanning this item")
				break
			}
			log.Info().
				Str("component", componentAddItemData).
				Str("item_id", itemID).
				Str("node", nodeName).
				Int("quantity", qty).
				Int("hit_index", hitIndex).
				Msg("masked hit region for further recognition")
		}
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
		Int("item_param_count", len(items)).
		Int("hit_count", len(hits)).
		Int("added_total", addedTotal).
		Bool("cache_ready", cacheReady).
		Bool("mask_hit_region", maskHitRegion).
		Bool("items_from_catalog", len(params.Items) == 0).
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

func workingRGBA(img image.Image) *image.RGBA {
	if img == nil {
		return nil
	}
	if rgba, ok := img.(*image.RGBA); ok {
		return minicv.ImageCopy(rgba)
	}
	return minicv.ImageConvertRGBA(img)
}

// paintItemHitRegion fills the recognized item card area with green so later
// TemplateMatch/ColorMatch (green_mask / quality color) skip it. Uses the union
// of CombinedResult[0] (quality color) and CombinedResult[1] (template) when
// present; falls back to detail.Box.
func paintItemHitRegion(img *image.RGBA, detail *maa.RecognitionDetail) bool {
	if img == nil || detail == nil {
		return false
	}
	box := itemHitMaskBox(detail)
	if box[2] <= 0 || box[3] <= 0 {
		return false
	}
	return fillRectColor(img, box, imsGreenMaskColor)
}

func itemHitMaskBox(detail *maa.RecognitionDetail) maa.Rect {
	if detail == nil {
		return maa.Rect{}
	}
	rects := make([]maa.Rect, 0, 2)
	if len(detail.CombinedResult) > 0 && detail.CombinedResult[0] != nil {
		rects = append(rects, detail.CombinedResult[0].Box)
	}
	if len(detail.CombinedResult) > 1 && detail.CombinedResult[1] != nil {
		rects = append(rects, detail.CombinedResult[1].Box)
	}
	if len(rects) == 0 {
		return detail.Box
	}
	return unionRects(rects...)
}

func unionRects(rects ...maa.Rect) maa.Rect {
	valid := make([]maa.Rect, 0, len(rects))
	for _, r := range rects {
		if r[2] > 0 && r[3] > 0 {
			valid = append(valid, r)
		}
	}
	if len(valid) == 0 {
		return maa.Rect{}
	}
	minX, minY := valid[0][0], valid[0][1]
	maxX, maxY := valid[0][0]+valid[0][2], valid[0][1]+valid[0][3]
	for _, r := range valid[1:] {
		if r[0] < minX {
			minX = r[0]
		}
		if r[1] < minY {
			minY = r[1]
		}
		if r[0]+r[2] > maxX {
			maxX = r[0] + r[2]
		}
		if r[1]+r[3] > maxY {
			maxY = r[1] + r[3]
		}
	}
	return maa.Rect{minX, minY, maxX - minX, maxY - minY}
}

func fillRectColor(img *image.RGBA, box maa.Rect, c color.RGBA) bool {
	if img == nil || box[2] <= 0 || box[3] <= 0 {
		return false
	}
	rect := image.Rect(box[0], box[1], box[0]+box[2], box[1]+box[3]).Intersect(img.Bounds())
	if rect.Empty() {
		return false
	}
	draw.Draw(img, rect, &image.Uniform{C: c}, image.Point{}, draw.Src)
	return true
}
