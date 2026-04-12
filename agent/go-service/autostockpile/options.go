package autostockpile

import (
	"encoding/json"
	"fmt"
	"strings"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

type serverTimeAttach struct {
	ServerTime *int `json:"server_time"`
}

const (
	minServerTimeOffset = -12
	maxServerTimeOffset = 14
)

func validateServerTimeOffset(offset *int) error {
	if offset == nil {
		return nil
	}
	if *offset < minServerTimeOffset || *offset > maxServerTimeOffset {
		return fmt.Errorf("server_time must be within [%d, %d], got %d", minServerTimeOffset, maxServerTimeOffset, *offset)
	}

	return nil
}

func loadServerTimeOffsetFromAttach(ctx *maa.Context, nodeName string) (*int, error) {
	if ctx == nil {
		return nil, fmt.Errorf("context is nil")
	}
	if strings.TrimSpace(nodeName) == "" {
		return nil, fmt.Errorf("node name is empty")
	}

	raw, err := ctx.GetNodeJSON(nodeName)
	if err != nil {
		return nil, fmt.Errorf("get node %s json: %w", nodeName, err)
	}

	var wrapper struct {
		Attach serverTimeAttach `json:"attach"`
	}
	if err := json.Unmarshal([]byte(raw), &wrapper); err != nil {
		return nil, fmt.Errorf("unmarshal %s attach: %w", nodeName, err)
	}
	if err := validateServerTimeOffset(wrapper.Attach.ServerTime); err != nil {
		return nil, fmt.Errorf("validate %s attach: %w", nodeName, err)
	}

	return wrapper.Attach.ServerTime, nil
}
