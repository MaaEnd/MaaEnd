package resourceoverride

import (
	"encoding/json"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

type resourceOverrideParam struct {
	// Patch maps node name to a partial node JSON object, merged into the
	// resource's base pipeline via Resource.OverridePipeline. The override
	// persists for the resource lifetime, so subsequent tasks inherit it.
	Patch map[string]interface{} `json:"patch"`
}

// ResourceOverridePipelineAction applies a pipeline override at the Resource
// level (Resource.OverridePipeline), making the patch survive across tasks
// within the same session. It is a generic primitive: TaskSettings is just its
// first consumer.
type ResourceOverridePipelineAction struct{}

// Compile-time interface check
var _ maa.CustomActionRunner = &ResourceOverridePipelineAction{}

func (a *ResourceOverridePipelineAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if arg == nil {
		log.Error().Str("component", "ResourceOverridePipeline").Msg("got nil custom action arg")
		return false
	}

	var params resourceOverrideParam
	if err := json.Unmarshal([]byte(arg.CustomActionParam), &params); err != nil {
		log.Error().
			Err(err).
			Str("component", "ResourceOverridePipeline").
			Int("custom_action_param_len", len(arg.CustomActionParam)).
			Msg("failed to parse custom_action_param")
		return false
	}

	if len(params.Patch) == 0 {
		log.Info().
			Str("component", "ResourceOverridePipeline").
			Msg("empty patch, nothing to override")
		return true
	}

	resource := ctx.GetTasker().GetResource()
	if err := resource.OverridePipeline(params.Patch); err != nil {
		log.Error().
			Err(err).
			Str("component", "ResourceOverridePipeline").
			Interface("patch_node_keys", keysOf(params.Patch)).
			Msg("Resource.OverridePipeline failed")
		return false
	}

	log.Info().
		Str("component", "ResourceOverridePipeline").
		Int("patch_node_count", len(params.Patch)).
		Interface("patch_node_keys", keysOf(params.Patch)).
		Msg("Resource.OverridePipeline applied successfully")

	return true
}

func keysOf(m map[string]interface{}) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	return ks
}
