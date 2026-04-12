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

	return wrapper.Attach.ServerTime, nil
}
