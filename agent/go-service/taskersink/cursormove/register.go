package cursormove

import (
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/pienv"
	"github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

var _ maa.ContextEventSink = &CursorMoveSink{}

// Register adds the cursor-move context sink when the controller is Win32.
func Register() {
	if pienv.ControllerType() != "Win32" {
		return
	}

	maa.AgentServerAddContextSink(&CursorMoveSink{})
	log.Info().
		Str("controller", pienv.ControllerName()).
		Msg("[cursormove] context sink registered for Win32 controller")
}
