package autostockpile

import (
	"encoding/json"
	"fmt"
	"strings"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

func resolveGoodsRegionFromTaskNode(ctx *maa.Context, taskName string) (string, error) {
	if ctx == nil {
		return "", fmt.Errorf("context is nil")
	}
	if strings.TrimSpace(taskName) == "" {
		return "", fmt.Errorf("task name is empty")
	}

	node, err := ctx.GetNode(taskName)
	if err != nil {
		return "", fmt.Errorf("get node %s: %w", taskName, err)
	}
	if node.Action == nil {
		return "", fmt.Errorf("node %s missing action", taskName)
	}

	param, ok := node.Action.Param.(*maa.CustomActionParam)
	if !ok || param == nil {
		return "", fmt.Errorf("node %s action param type %T is not *maa.CustomActionParam", taskName, node.Action.Param)
	}

	return resolveGoodsRegionFromCustomActionParam(param.CustomActionParam)
}

func resolveGoodsRegionFromActionArg(arg *maa.CustomActionArg) (string, error) {
	if arg == nil {
		return "", fmt.Errorf("custom action arg is nil")
	}

	return resolveGoodsRegionFromCustomActionParam(arg.CustomActionParam)
}

func resolveGoodsRegionFromCustomActionParam(raw any) (string, error) {
	if raw == nil {
		return "", fmt.Errorf("custom_action_param is nil")
	}

	param, err := normalizeCustomActionParam(raw)
	if err != nil {
		return "", fmt.Errorf("normalize custom_action_param: %w", err)
	}

	regionValue, ok := param["Region"]
	if !ok {
		return "", fmt.Errorf("custom_action_param.Region is required")
	}

	region, ok := regionValue.(string)
	if !ok {
		return "", fmt.Errorf("custom_action_param.Region type %T is not string", regionValue)
	}
	region = strings.TrimSpace(region)
	if region == "" {
		return "", fmt.Errorf("custom_action_param.Region is empty")
	}

	itemMap := GetItemMap()
	if err := validateItemMap(itemMap); err != nil {
		return "", fmt.Errorf("item_map unavailable: %w", err)
	}
	if !itemMapHasRegion(itemMap, region) {
		return "", fmt.Errorf("custom_action_param.Region %q not found in item_map", region)
	}

	return region, nil
}

func loadSwipeSpecificQuantityCustomActionParam(ctx *maa.Context) (map[string]any, error) {
	if ctx == nil {
		return nil, fmt.Errorf("context is nil")
	}

	node, err := ctx.GetNode(swipeSpecificQuantityNodeName)
	if err != nil {
		return nil, err
	}

	if node.Action == nil {
		return nil, fmt.Errorf("node %s missing action", swipeSpecificQuantityNodeName)
	}

	param, ok := node.Action.Param.(*maa.CustomActionParam)
	if !ok || param == nil {
		return nil, fmt.Errorf("node %s action param type %T is not *maa.CustomActionParam", swipeSpecificQuantityNodeName, node.Action.Param)
	}

	return normalizeCustomActionParam(param.CustomActionParam)
}

func normalizeCustomActionParam(raw any) (map[string]any, error) {
	switch value := raw.(type) {
	case map[string]any:
		cloned := make(map[string]any, len(value))
		for key, item := range value {
			cloned[key] = item
		}
		return cloned, nil
	case string:
		var nested any
		if err := json.Unmarshal([]byte(value), &nested); err != nil {
			return nil, err
		}
		return normalizeCustomActionParam(nested)
	default:
		return nil, fmt.Errorf("unsupported custom_action_param type %T", raw)
	}
}
