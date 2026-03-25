package cursormove

import (
	"sync/atomic"

	"github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

// CursorMoveSink moves the cursor to (0,0) before the next recognition cycle
// when the previous action repositioned the cursor. This prevents the cursor
// from occluding game UI elements during Win32 screencap.
type CursorMoveSink struct {
	dirty atomic.Bool
}

func (s *CursorMoveSink) OnNodeAction(ctx *maa.Context, event maa.EventStatus, detail maa.NodeActionDetail) {
	if event != maa.EventStatusSucceeded && event != maa.EventStatusFailed {
		return
	}

	ad, err := ctx.GetTasker().GetActionDetail(int64(detail.ActionID))
	if err != nil || ad == nil {
		return
	}

	switch ad.Action {
	case "Click", "Swipe", "Scroll", "Custom":
		s.dirty.Store(true)
	}
}

func (s *CursorMoveSink) OnNodeNextList(ctx *maa.Context, event maa.EventStatus, _ maa.NodeNextListDetail) {
	if event != maa.EventStatusStarting {
		return
	}

	if !s.dirty.CompareAndSwap(true, false) {
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
