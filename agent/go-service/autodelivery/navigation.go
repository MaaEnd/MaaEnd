package autodelivery

import (
	"encoding/json"
	"fmt"
	"strings"

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
