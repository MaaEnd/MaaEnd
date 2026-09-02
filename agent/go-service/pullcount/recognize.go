package pullcount

import (
	"fmt"
	"image"
	"sort"
	"strings"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/ocrnum"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/recogtarget"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const (
	itemDiamond    = "item_diamond"
	itemOriginium  = "item_originium_recharge"
	itemSpecial    = "item_ticketgacha_special_single"
	itemSpecialLT  = "item_ticketgacha_special_single_1_lt"
	itemSpecialTen = "item_ticketgacha_special_ten"
)

// voucherPulls maps catalog item IDs to recruitment pulls.
// Pipeline items keys should use these IDs.
// item_diamond / item_originium_recharge are IMS-only.
var voucherPulls = map[string]int{
	itemSpecial:    1,
	itemSpecialLT:  1,
	itemSpecialTen: 10,
}

// readQuantityFromNode runs a Pipeline And recognizer whose box_index
// points at an OCR digit result, then parses the number.
func readQuantityFromNode(ctx *maa.Context, img image.Image, node string) (qty int, hit bool, err error) {
	node = strings.TrimSpace(node)
	if node == "" {
		return 0, false, fmt.Errorf("recognition node is empty")
	}
	if ctx == nil {
		return 0, false, fmt.Errorf("context is nil")
	}

	detail, err := ctx.RunRecognition(node, img)
	if err != nil {
		return 0, false, fmt.Errorf("run recognition %s: %w", node, err)
	}
	if detail == nil || !detail.Hit {
		log.Info().
			Str("component", componentName).
			Str("node", node).
			Msg("recognizer not hit")
		return 0, false, nil
	}

	selected, err := recogtarget.SelectDetail(ctx, node, detail)
	if err != nil {
		return 0, false, fmt.Errorf("select box_index detail for %s: %w", node, err)
	}
	qty, err = ocrnum.Extract(selected)
	if err != nil {
		return 0, false, fmt.Errorf("parse quantity from %s: %w", node, err)
	}
	return qty, true, nil
}

func sortedItemIDs(items map[string]string) []string {
	ids := make([]string, 0, len(items))
	for id := range items {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func cacheScreenImage(ctx *maa.Context) (image.Image, error) {
	if ctx == nil {
		return nil, fmt.Errorf("context is nil")
	}
	tasker := ctx.GetTasker()
	if tasker == nil || tasker.GetController() == nil {
		return nil, fmt.Errorf("tasker or controller is nil")
	}
	img, err := tasker.GetController().CacheImage()
	if err != nil {
		return nil, fmt.Errorf("cache image: %w", err)
	}
	if img == nil {
		return nil, fmt.Errorf("cache image is nil")
	}
	return img, nil
}
