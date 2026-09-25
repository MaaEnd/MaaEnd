package autodelivery

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/maafocus"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

type navigationOptions struct {
	Zip bool `json:"zip"`
}

type destinationSelection struct {
	DestinationID string `json:"destination_id"`
}

func loadNavigationOptions(ctx *maa.Context, nodeName string) (navigationOptions, error) {
	if ctx == nil {
		return navigationOptions{}, fmt.Errorf("context is nil")
	}
	if strings.TrimSpace(nodeName) == "" {
		return navigationOptions{}, fmt.Errorf("node name is empty")
	}

	raw, err := ctx.GetNodeJSON(nodeName)
	if err != nil {
		return navigationOptions{}, fmt.Errorf("get node %s json: %w", nodeName, err)
	}
	return parseNavigationOptions(raw, nodeName)
}

func parseNavigationOptions(raw string, nodeName string) (navigationOptions, error) {
	var node struct {
		Attach navigationOptions `json:"attach"`
	}
	if err := json.Unmarshal([]byte(raw), &node); err != nil {
		return navigationOptions{}, fmt.Errorf("unmarshal %s attach: %w", nodeName, err)
	}
	return node.Attach, nil
}

func parseDestinationSelection(paramJSON string) (destinationSelection, error) {
	if paramJSON == "" {
		return destinationSelection{}, nil
	}

	var selection destinationSelection
	if err := json.Unmarshal([]byte(paramJSON), &selection); err != nil {
		return destinationSelection{}, fmt.Errorf("unmarshal parameters: %w", err)
	}
	selection.DestinationID = strings.TrimSpace(selection.DestinationID)
	return selection, nil
}

// ensureZiplineSelected 拦截「只能通过滑索抵达、但用户选择步行」的路线。
// 这类目标（如裴令容）没有可用的步行路线，静默按步行执行只会走到不可达处再超时，
// 所以这里把原因讲给用户并让动作失败；是否记日志由调用方决定。
func ensureZiplineSelected(ctx *maa.Context, ziplineOnly bool, zip bool, focusKey string, displayName string) bool {
	if !ziplineOnly || zip {
		return true
	}
	maafocus.Print(ctx, i18n.T(focusKey, displayName))
	return false
}
