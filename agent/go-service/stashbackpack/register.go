// Package stashbackpack selects untried quick-move targets from the native Pipeline results.
package stashbackpack

import (
	"encoding/json"
	"image"
	"strings"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/recogtarget"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const (
	component  = "StashBackpackNextItem"
	targetNode = "StashBackpackUsableItemsLimitedTarget"
)

var _ maa.CustomRecognitionRunner = maa.CustomRecognitionFunc(nextItem)

// Register registers the Win32 quick-move candidate filter.
func Register() {
	maa.AgentServerRegisterCustomRecognition(component, maa.CustomRecognitionFunc(nextItem))
}

func nextItem(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	if ctx == nil || arg == nil || arg.Img == nil {
		return nil, false
	}
	raw, err := ctx.GetNodeJSON(arg.CurrentTaskName)
	if err != nil {
		return failed(err)
	}
	var node struct {
		Attach struct {
			Attempted []maa.Rect `json:"attempted"`
		} `json:"attach"`
	}
	if err := json.Unmarshal([]byte(raw), &node); err != nil {
		return failed(err)
	}

	detail, err := ctx.RunRecognition(targetNode, arg.Img)
	if err != nil {
		return failed(err)
	}
	if detail == nil || !detail.Hit {
		return nil, false
	}
	icons, err := recogtarget.SelectedDetail(detail, 0)
	if err != nil {
		return failed(err)
	}
	quantities, err := recogtarget.SelectedDetail(detail, 1)
	if err != nil {
		return failed(err)
	}
	if icons.Results == nil || quantities.Results == nil {
		return nil, false
	}

	icon, box, ok := bestTarget(icons.Results.Filtered, quantities.Results.Filtered, node.Attach.Attempted)
	if !ok {
		return nil, false
	}
	if err := ctx.OverridePipeline(map[string]any{
		arg.CurrentTaskName: map[string]any{"attach": map[string]any{
			"attempted": append(node.Attach.Attempted, icon),
		}},
	}); err != nil {
		return failed(err)
	}
	return &maa.CustomRecognitionResult{Box: box}, true
}

func bestTarget(icons, quantities []*maa.RecognitionResult, attempted []maa.Rect) (iconBox, targetBox maa.Rect, found bool) {
	var bestScore float64
	for _, result := range icons {
		if result == nil {
			continue
		}
		icon, ok := result.AsTemplateMatch()
		if !ok || (found && icon.Score <= bestScore) || alreadyAttempted(icon.Box, attempted) {
			continue
		}
		// Same quantity region as the shared Pipeline target's roi_offset.
		x, y, w, h := icon.Box[0]+20, icon.Box[1]+44, icon.Box[2]+4, icon.Box[3]-2
		region := image.Rect(x, y, x+w, y+h)
		for _, result := range quantities {
			if result == nil {
				continue
			}
			quantity, ok := result.AsOCR()
			if !ok || !center(quantity.Box).In(region) {
				continue
			}
			text := strings.TrimSpace(quantity.Text)
			if len(text) != 1 || text[0] < '0' || text[0] > '4' {
				continue
			}
			iconBox, targetBox, bestScore, found = icon.Box, quantity.Box, icon.Score, true
			break
		}
	}
	return
}

func alreadyAttempted(box maa.Rect, attempted []maa.Rect) bool {
	point := center(box)
	for _, old := range attempted {
		if point.In(image.Rect(old[0], old[1], old[0]+old[2], old[1]+old[3])) {
			return true
		}
	}
	return false
}

func center(box maa.Rect) image.Point {
	return image.Pt(box[0]+box[2]/2, box[1]+box[3]/2)
}

func failed(err error) (*maa.CustomRecognitionResult, bool) {
	log.Error().Err(err).Str("component", component).Msg("选择背包快移目标失败")
	return nil, false
}
