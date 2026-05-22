package pullcount

import (
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

var _ maa.CustomRecognitionRunner = &Recognition{}

// Recognition exposes pull-count branch decisions as Pipeline recognition nodes.
type Recognition struct{}

// Run lets Pipeline choose the next scan branch without Go mutating the next graph.
func (r *Recognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	if ctx == nil || arg == nil {
		log.Error().Str("component", componentName).Msg("custom recognition context or arg is nil")
		return nil, false
	}

	param, err := parseActionParam(arg.CustomRecognitionParam)
	if err != nil {
		log.Error().
			Err(err).
			Str("component", componentName).
			Str("custom_recognition_param", arg.CustomRecognitionParam).
			Msg("failed to parse recognition params")
		return nil, false
	}

	sessionMu.Lock()
	defer sessionMu.Unlock()

	if currentSession == nil {
		log.Error().Str("component", componentName).Msg("missing session for custom recognition")
		return nil, false
	}

	switch param.Stage {
	case stagePageShouldFinish:
		return pageShouldFinishResult(arg, currentSession)
	default:
		log.Error().Str("component", componentName).Str("stage", param.Stage).Msg("unknown recognition stage")
		return nil, false
	}
}

// pageShouldFinishResult matches when the just-recorded warehouse page should end scanning.
func pageShouldFinishResult(arg *maa.CustomRecognitionArg, session *runSession) (*maa.CustomRecognitionResult, bool) {
	if !session.StopAfterPageDone {
		return nil, false
	}
	log.Info().
		Str("component", componentName).
		Int("page_count", session.PageCount).
		Str("reason", session.PageStopReason).
		Msg("warehouse page finish branch matched")
	return customRecognitionResult(arg, session.PageStopReason)
}

// customRecognitionResult returns a DirectHit-like custom result with a diagnostic detail string.
func customRecognitionResult(arg *maa.CustomRecognitionArg, detail string) (*maa.CustomRecognitionResult, bool) {
	return &maa.CustomRecognitionResult{
		Box:    arg.Roi,
		Detail: detail,
	}, true
}
