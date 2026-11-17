package pipelineoverride

import (
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

func Register() {
	maa.AgentServerRegisterCustomAction("PipelineOverride", &PipelineOverrideAction{})
	log.Info().
		Str("component", "PipelineOverride").
		Msg("registered custom action: PipelineOverride")
}
