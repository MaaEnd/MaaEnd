package useconsumable

import (
	"encoding/json"
	"fmt"
	"strings"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const componentName = "UseConsumableUniqueItemRecognition"

type UniqueItemRecognition struct{}

var _ maa.CustomRecognitionRunner = &UniqueItemRecognition{}

func (r *UniqueItemRecognition) Run(
	ctx *maa.Context,
	arg *maa.CustomRecognitionArg,
) (*maa.CustomRecognitionResult, bool) {
	if ctx == nil || arg == nil {
		return nil, false
	}

	var params struct {
		Slot int `json:"slot"`
	}
	if err := json.Unmarshal([]byte(arg.CustomRecognitionParam), &params); err != nil {
		log.Error().Err(err).Str("component", componentName).Msg("failed to parse slot parameter")
		return nil, false
	}
	if params.Slot < 1 || params.Slot > 4 {
		log.Error().Str("component", componentName).Int("slot", params.Slot).Msg("slot is outside 1..4")
		return nil, false
	}

	currentEnabled, err := nodeEnabled(ctx, fmt.Sprintf("UseConsumableSlot%dEnabled", params.Slot))
	if err != nil {
		log.Error().Err(err).Str("component", componentName).Int("slot", params.Slot).Msg("failed to read slot state")
		return nil, false
	}
	if !currentEnabled {
		return nil, false
	}
	itemID, err := selectedItemID(ctx, params.Slot)
	if err != nil {
		log.Error().Err(err).Str("component", componentName).Int("slot", params.Slot).Msg("failed to read selected item")
		return nil, false
	}

	for slot := 1; slot < params.Slot; slot++ {
		enabled, err := nodeEnabled(ctx, fmt.Sprintf("UseConsumableSlot%dEnabled", slot))
		if err != nil {
			log.Error().Err(err).Str("component", componentName).Int("slot", slot).Msg("failed to read slot state")
			return nil, false
		}
		if !enabled {
			continue
		}
		previousItemID, err := selectedItemID(ctx, slot)
		if err != nil {
			log.Error().Err(err).Str("component", componentName).Int("slot", slot).Msg("failed to read selected item")
			return nil, false
		}
		if itemID == previousItemID {
			return nil, false
		}
	}

	return &maa.CustomRecognitionResult{Box: arg.Roi}, true
}

func nodeEnabled(ctx *maa.Context, nodeName string) (bool, error) {
	raw, err := ctx.GetNodeJSON(nodeName)
	if err != nil {
		return false, fmt.Errorf("get %s: %w", nodeName, err)
	}
	var node struct {
		Enabled *bool `json:"enabled"`
	}
	if err := json.Unmarshal([]byte(raw), &node); err != nil {
		return false, fmt.Errorf("parse %s: %w", nodeName, err)
	}
	return node.Enabled == nil || *node.Enabled, nil
}

func selectedItemID(ctx *maa.Context, slot int) (string, error) {
	nodeName := fmt.Sprintf("UseConsumableFindItemSlot%d", slot)
	raw, err := ctx.GetNodeJSON(nodeName)
	if err != nil {
		return "", fmt.Errorf("get %s: %w", nodeName, err)
	}
	var node struct {
		Recognition struct {
			Param struct {
				CustomRecognitionParam struct {
					ItemIDs []string `json:"item_ids"`
				} `json:"custom_recognition_param"`
			} `json:"param"`
		} `json:"recognition"`
	}
	if err := json.Unmarshal([]byte(raw), &node); err != nil {
		return "", fmt.Errorf("parse %s: %w", nodeName, err)
	}
	itemIDs := node.Recognition.Param.CustomRecognitionParam.ItemIDs
	if len(itemIDs) != 1 || strings.TrimSpace(itemIDs[0]) == "" {
		return "", fmt.Errorf("%s must have exactly one selected item id", nodeName)
	}
	return strings.TrimSpace(itemIDs[0]), nil
}
