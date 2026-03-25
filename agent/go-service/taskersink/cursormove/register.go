package cursormove

import (
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/pienv"
	"github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

// Register adds the cursor-move sinks when the controller is Win32.
func Register() {
	if pienv.ControllerName() != "Win32-Front" {
		return
	}

	sink := &CursorMoveSink{}
	maa.AgentServerAddContextSink(sink)
	maa.AgentServerAddTaskerSink(sink)
	log.Info().
		Str("component", "cursormove").
		Str("controller", pienv.ControllerName()).
		Msg("sinks registered for Win32 controller")
}
