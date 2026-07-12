package cursormove

import (
	"github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

var (
	_ maa.ContextEventSink = &CursorMoveSink{}
	_ maa.TaskerEventSink  = &CursorMoveSink{}
)

// CursorMoveSink moves the cursor to the bottom-right corner before the next
// recognition cycle when the previous action repositioned the cursor.
// This prevents the cursor from occluding game UI elements during Win32 screencap.
type CursorMoveSink struct {
	dirty bool
	imgW  int32
	imgH  int32
}

func (s *CursorMoveSink) moveCursor(ctrl *maa.Controller) {
	ctrl.PostTouchMove(0, s.imgW, s.imgH, 0).Wait()
}

func (s *CursorMoveSink) OnTaskerTask(tasker *maa.Tasker, event maa.EventStatus, _ maa.TaskerTaskDetail) {
	if event != maa.EventStatusStarting {
		return
	}

	ctrl := tasker.GetController()
	if ctrl == nil {
		log.Warn().Str("component", "cursormove").Msg("failed to get controller from tasker")
		return
	}

	img, err := ctrl.CacheImage()
	if err != nil || img == nil {
		log.Warn().Err(err).Str("component", "cursormove").Msg("failed to get cached image for size")
		return
	}

	bounds := img.Bounds()
	s.imgW = int32(bounds.Dx())
	s.imgH = int32(bounds.Dy())
	log.Info().
		Str("component", "cursormove").
		Int32("width", s.imgW).
		Int32("height", s.imgH).
		Msg("captured image size")

	s.moveCursor(ctrl)
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
		s.dirty = true
	}
}

func (s *CursorMoveSink) OnNodeNextList(ctx *maa.Context, event maa.EventStatus, _ maa.NodeNextListDetail) {
	if event != maa.EventStatusStarting {
		return
	}

	if !s.dirty {
		return
	}
	s.dirty = false

	ctrl := ctx.GetTasker().GetController()
	if ctrl == nil {
		log.Warn().Str("component", "cursormove").Msg("failed to get controller from context")
		return
	}

	s.moveCursor(ctrl)
}

func (s *CursorMoveSink) OnNodePipelineNode(_ *maa.Context, _ maa.EventStatus, _ maa.NodePipelineNodeDetail) {
}

func (s *CursorMoveSink) OnNodeRecognitionNode(_ *maa.Context, _ maa.EventStatus, _ maa.NodeRecognitionNodeDetail) {
}

func (s *CursorMoveSink) OnNodeActionNode(_ *maa.Context, _ maa.EventStatus, _ maa.NodeActionNodeDetail) {
}

func (s *CursorMoveSink) OnNodeRecognition(_ *maa.Context, _ maa.EventStatus, _ maa.NodeRecognitionDetail) {
}
