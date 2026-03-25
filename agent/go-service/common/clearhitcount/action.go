package clearhitcount

import (
	"encoding/json"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

type clearHitCountParam struct {
	Nodes  []string `json:"nodes"`            // 要清除命中计数的节点名称列表
	Strict *bool    `json:"strict,omitempty"` // 是否严格模式，任一节点清除失败时 action 视为失败。可选字段，默认 false
}

type ClearHitCountAction struct{}

// Compile-time interface check
var _ maa.CustomActionRunner = &ClearHitCountAction{}

func (a *ClearHitCountAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if arg == nil {
		log.Error().Msg("ClearHitCount got nil custom action arg")
		return false
	}

	var params clearHitCountParam
	if err := json.Unmarshal([]byte(arg.CustomActionParam), &params); err != nil {
		log.Error().
			Err(err).
			Str("param", arg.CustomActionParam).
			Msg("ClearHitCount failed to parse custom_action_param")
		return false
	}

	if len(params.Nodes) == 0 {
		log.Error().Msg("ClearHitCount requires non-empty custom_action_param.nodes")
		return false
	}

	// 解析 strict 参数，默认为 false（非严格模式）
	strictMode := false
	if params.Strict != nil {
		strictMode = *params.Strict
	}

	hasFailure := false

	// 清除所有指定节点的命中计数
	for i, nodeName := range params.Nodes {
		if nodeName == "" {
			if strictMode {
				log.Error().
					Int("index", i).
					Msg("ClearHitCount received empty node name in custom_action_param.nodes")
			} else {
				log.Warn().
					Int("index", i).
					Msg("ClearHitCount received empty node name in custom_action_param.nodes")
			}
			hasFailure = true
			continue
		}

		if err := ctx.ClearHitCount(nodeName); err != nil {
			if strictMode {
				log.Error().
					Err(err).
					Int("index", i).
					Str("node", nodeName).
					Msg("ClearHitCount failed to clear hit count")
			} else {
				log.Warn().
					Err(err).
					Int("index", i).
					Str("node", nodeName).
					Msg("ClearHitCount failed to clear hit count")
			}
			hasFailure = true
			continue
		}

		log.Info().
			Str("node", nodeName).
			Msg("ClearHitCount successfully cleared hit count")
	}

	// 如果有失败且是严格模式，则返回 false
	if hasFailure && strictMode {
		log.Error().
			Msg("ClearHitCount failed to clear some nodes in strict mode")
		return false
	}

	return true
}
