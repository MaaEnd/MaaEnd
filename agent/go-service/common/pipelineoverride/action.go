package pipelineoverride

import (
	"encoding/json"
	"maps"
	"strings"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

type pipelineOverrideParam struct {
	// Patch maps node name to a partial node JSON object (merged by framework). Required.
	Patch map[string]interface{} `json:"patch"`
	// AllowNext when true allows patch entries to include top-level "next" on each node.
	// When false (default), "next" is removed before OverridePipeline to keep topology preset.
	AllowNext *bool `json:"allow_next,omitempty"`
	// Strict when true and allow_next is false: fail the action if any node patch contained "next".
	Strict *bool `json:"strict,omitempty"`
}

// PipelineOverrideAction applies ctx.OverridePipeline from JSON param.
// By default it strips per-node "next" so runtime changes stay within preset flow topology.
type PipelineOverrideAction struct{}

// Compile-time interface check
var _ maa.CustomActionRunner = &PipelineOverrideAction{}

func (a *PipelineOverrideAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if arg == nil {
		log.Error().Str("component", "PipelineOverride").Msg("got nil custom action arg")
		return false
	}

	customParam := arg.CustomActionParam
	log.Debug().
		Str("component", "PipelineOverride").
		Int("custom_action_param_len", len(customParam)).
		Bool("custom_action_param_present", strings.TrimSpace(customParam) != "").
		Msg("PipelineOverride Run invoked")

	var params pipelineOverrideParam
	if err := json.Unmarshal([]byte(arg.CustomActionParam), &params); err != nil {
		log.Error().
			Err(err).
			Str("component", "PipelineOverride").
			Int("custom_action_param_len", len(arg.CustomActionParam)).
			Msg("failed to parse custom_action_param")
		return false
	}

	log.Debug().
		Str("component", "PipelineOverride").
		Int("patch_node_count_raw", len(params.Patch)).
		Msg("parsed custom_action_param")

	if len(params.Patch) == 0 {
		log.Error().Str("component", "PipelineOverride").Msg("requires non-empty custom_action_param.patch")
		return false
	}

	allowNext := false
	if params.AllowNext != nil {
		allowNext = *params.AllowNext
	}

	strictMode := false
	if params.Strict != nil {
		strictMode = *params.Strict
	}
	if allowNext && strictMode {
		log.Info().
			Str("component", "PipelineOverride").
			Msg("strict is ignored because allow_next is true")
		strictMode = false
	}

	log.Debug().
		Str("component", "PipelineOverride").
		Bool("allow_next", allowNext).
		Bool("strict", strictMode).
		Msg("PipelineOverride config")

	cleanPatch := make(map[string]interface{}, len(params.Patch))

	for nodeName, raw := range params.Patch {
		if strings.TrimSpace(nodeName) == "" {
			log.Error().
				Str("component", "PipelineOverride").
				Msg("patch contains empty node name key")
			return false
		}

		nodeObj, ok := raw.(map[string]interface{})
		if !ok {
			log.Error().
				Str("component", "PipelineOverride").
				Str("node", nodeName).
				Msg("patch entry must be a JSON object")
			return false
		}

		cloned := maps.Clone(nodeObj)
		if !allowNext {
			if _, hadNext := nodeObj["next"]; hadNext {
				log.Debug().
					Str("component", "PipelineOverride").
					Str("node", nodeName).
					Bool("strict", strictMode).
					Msg("patch contains next key")
				if strictMode {
					log.Error().
						Str("component", "PipelineOverride").
						Str("node", nodeName).
						Msg("patch contained next while allow_next is false (strict)")
					return false
				}
				delete(cloned, "next")
				log.Info().
					Str("component", "PipelineOverride").
					Str("node", nodeName).
					Msg("stripped next from patch (allow_next is false)")
			}
		}

		cleanPatch[nodeName] = cloned
	}

	log.Debug().
		Str("component", "PipelineOverride").
		Int("patch_node_count_clean", len(cleanPatch)).
		Interface("patch_node_keys", keysOf(cleanPatch)).
		Msg("prepared cleanPatch; calling ctx.OverridePipeline")

	if err := ctx.OverridePipeline(cleanPatch); err != nil {
		log.Error().
			Err(err).
			Str("component", "PipelineOverride").
			Int("patch_node_count_clean", len(cleanPatch)).
			Interface("patch_node_keys", keysOf(cleanPatch)).
			Msg("OverridePipeline failed")
		return false
	}

	log.Info().
		Str("component", "PipelineOverride").
		Int("patch_node_count_clean", len(cleanPatch)).
		Interface("nodes", keysOf(cleanPatch)).
		Msg("OverridePipeline applied successfully")

	return true
}

func keysOf(m map[string]interface{}) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	return ks
}
