package itemtransfer

import (
	"encoding/json"
	"fmt"
	"strings"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

type sameItemRecognitionParam struct {
	ForwardItemNode string `json:"forward_item_node"`
	ReturnItemNode  string `json:"return_item_node"`
}

type sameItemNodeStore interface {
	GetNodeJSON(nodeName string) (string, error)
}

type selectedItemNode struct {
	Recognition struct {
		Param struct {
			CustomRecognitionParam struct {
				ItemIDs []string `json:"item_ids"`
			} `json:"custom_recognition_param"`
		} `json:"param"`
	} `json:"recognition"`
}

// SameItemRecognition 判断双向搬运配置中的去程和返程物品是否相同。
type SameItemRecognition struct{}

var _ maa.CustomRecognitionRunner = &SameItemRecognition{}

// Run 解析两个方向的物品 ID，仅在二者均有效且相同时命中。
func (r *SameItemRecognition) Run(
	ctx *maa.Context,
	arg *maa.CustomRecognitionArg,
) (*maa.CustomRecognitionResult, bool) {
	if ctx == nil {
		log.Error().Str("component", componentName).Msg("same item recognition received nil context")
		return nil, false
	}
	return runSameItemRecognition(ctx, arg)
}

func runSameItemRecognition(
	store sameItemNodeStore,
	arg *maa.CustomRecognitionArg,
) (*maa.CustomRecognitionResult, bool) {
	if arg == nil {
		log.Error().Str("component", componentName).Msg("same item recognition received nil arg")
		return nil, false
	}

	var param sameItemRecognitionParam
	if err := json.Unmarshal([]byte(arg.CustomRecognitionParam), &param); err != nil {
		log.Error().
			Err(err).
			Str("component", componentName).
			Msg("failed to parse same item recognition param")
		return nil, false
	}
	if param.ForwardItemNode == "" || param.ReturnItemNode == "" {
		log.Error().
			Str("component", componentName).
			Bool("forward_item_node_present", param.ForwardItemNode != "").
			Bool("return_item_node_present", param.ReturnItemNode != "").
			Msg("same item recognition requires both item nodes")
		return nil, false
	}

	// 两个下拉选项分别覆盖各自的找物节点，读取最终节点可避免它们争抢同一个参数对象。
	forwardItemID, err := loadSelectedItemID(store, param.ForwardItemNode)
	if err != nil {
		log.Error().
			Err(err).
			Str("component", componentName).
			Str("direction", "forward").
			Str("node", param.ForwardItemNode).
			Msg("failed to load selected item id")
		return nil, false
	}
	returnItemID, err := loadSelectedItemID(store, param.ReturnItemNode)
	if err != nil {
		log.Error().
			Err(err).
			Str("component", componentName).
			Str("direction", "return").
			Str("node", param.ReturnItemNode).
			Msg("failed to load selected item id")
		return nil, false
	}
	if forwardItemID != returnItemID {
		return nil, false
	}

	return &maa.CustomRecognitionResult{Box: arg.Roi}, true
}

func loadSelectedItemID(store sameItemNodeStore, nodeName string) (string, error) {
	raw, err := store.GetNodeJSON(nodeName)
	if err != nil {
		return "", fmt.Errorf("get node json: %w", err)
	}
	if strings.TrimSpace(raw) == "" {
		return "", fmt.Errorf("node json is empty")
	}

	var node selectedItemNode
	if err := json.Unmarshal([]byte(raw), &node); err != nil {
		return "", fmt.Errorf("unmarshal node json: %w", err)
	}
	itemIDs := node.Recognition.Param.CustomRecognitionParam.ItemIDs
	if len(itemIDs) != 1 || strings.TrimSpace(itemIDs[0]) == "" {
		return "", fmt.Errorf("expected exactly one selected item id, got %d", len(itemIDs))
	}
	return itemIDs[0], nil
}
