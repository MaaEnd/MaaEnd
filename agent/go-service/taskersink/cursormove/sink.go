package cursormove

import (
	"github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

// CursorMoveSink resets the cursor to (0,0) on every Node.NextList.Starting
// event. This prevents the Win32 controller's SendMessage-based mouse input
// from being blocked by the physical cursor sitting inside the game window.
type CursorMoveSink struct{}

func (s *CursorMoveSink) OnNodeNextList(ctx *maa.Context, event maa.EventStatus, _ maa.NodeNextListDetail) {
	if event != maa.EventStatusStarting {
		return
	}

	ctrl := ctx.GetTasker().GetController()
	if ctrl == nil {
		log.Warn().Msg("[cursormove] failed to get controller from context")
		return
	}

	ctrl.PostTouchMove(0, 0, 0, 0)
}

func (s *CursorMoveSink) OnNodePipelineNode(_ *maa.Context, _ maa.EventStatus, _ maa.NodePipelineNodeDetail) {
}

func (s *CursorMoveSink) OnNodeRecognitionNode(_ *maa.Context, _ maa.EventStatus, _ maa.NodeRecognitionNodeDetail) {
}

func (s *CursorMoveSink) OnNodeActionNode(_ *maa.Context, _ maa.EventStatus, _ maa.NodeActionNodeDetail) {
}

func (s *CursorMoveSink) OnNodeRecognition(_ *maa.Context, _ maa.EventStatus, _ maa.NodeRecognitionDetail) {
}

func (s *CursorMoveSink) OnNodeAction(_ *maa.Context, _ maa.EventStatus, _ maa.NodeActionDetail) {
}
