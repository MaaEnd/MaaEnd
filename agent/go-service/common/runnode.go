package common

import (
	"encoding/json"
	"strings"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

// RunNode 按传入节点名执行对应节点。
// custom_action_param 为 JSON，必须包含：
// - node_name: 需要执行的节点名。
type RunNode struct{}

var _ maa.CustomActionRunner = (*RunNode)(nil)

// Run 实现 maa.CustomActionRunner。
// 参数中 node_name 为空时返回 false；节点运行失败时记录错误并返回 false。
func (a *RunNode) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	var params struct {
		NodeName string `json:"node_name"`
	}

	rawParam := strings.TrimSpace(arg.CustomActionParam)
	if rawParam == "" {
		log.Error().Msg("[RunNode] custom_action_param is empty")
		return false
	}

	if err := json.Unmarshal([]byte(rawParam), &params); err != nil {
		log.Error().
			Err(err).
			Str("raw_param", rawParam).
			Msg("[RunNode] failed to parse custom_action_param")
		return false
	}

	nodeName := strings.TrimSpace(params.NodeName)
	if nodeName == "" {
		log.Error().
			Str("raw_param", rawParam).
			Msg("[RunNode] node_name is required")
		return false
	}

	if _, err := ctx.RunTask(nodeName); err != nil {
		log.Error().
			Err(err).
			Str("node_name", nodeName).
			Msg("[RunNode] failed to run node")
		return false
	}

	log.Info().
		Str("node_name", nodeName).
		Msg("[RunNode] node executed")

	return true
}
