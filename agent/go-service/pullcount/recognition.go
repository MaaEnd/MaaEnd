package pullcount

import (
	"encoding/json"
	"fmt"
	"strings"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

var _ maa.CustomRecognitionRunner = &Recognition{}

// Recognition exposes pull-count branch decisions as Pipeline recognition nodes.
type Recognition struct{}

type recognitionParam struct {
	Stage string `json:"stage"`
}

// Run lets Pipeline choose the next scan branch without Go mutating the next graph.
func (r *Recognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	if ctx == nil || arg == nil {
		log.Error().Str("component", componentName).Msg("custom recognition context or arg is nil")
		return nil, false
	}

	param, err := parseRecognitionParam(arg.CustomRecognitionParam)
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

	session, ok := requireRecognitionSession()
	if !ok {
		return nil, false
	}

	switch param.Stage {
	case stagePageShouldFinish:
		return pageShouldFinishResult(arg, session)
	case stageScrollProbeUnchanged:
		return scrollProbeUnchangedResult(arg, session)
	default:
		log.Error().Str("component", componentName).Str("stage", param.Stage).Msg("unknown recognition stage")
		return nil, false
	}
}

// parseRecognitionParam reads the branch decision requested by Pipeline.
func parseRecognitionParam(raw string) (*recognitionParam, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, fmt.Errorf("stage is required")
	}

	var param recognitionParam
	if err := json.Unmarshal([]byte(raw), &param); err != nil {
		return nil, err
	}
	param.Stage = strings.TrimSpace(param.Stage)
	if param.Stage == "" {
		return nil, fmt.Errorf("stage is required")
	}
	return &param, nil
}

// requireRecognitionSession returns the current session without user-facing focus noise.
func requireRecognitionSession() (*runSession, bool) {
	if currentSession != nil {
		return currentSession, true
	}
	log.Error().Str("component", componentName).Msg("missing session for custom recognition")
	return nil, false
}

// pageShouldFinishResult matches when the just-recorded warehouse page should end scanning.
func pageShouldFinishResult(arg *maa.CustomRecognitionArg, session *runSession) (*maa.CustomRecognitionResult, bool) {
	if !session.StopAfterPageDone {
		return nil, false
	}
	detail := map[string]any{
		"stage":      stagePageShouldFinish,
		"page_count": session.PageCount,
		"reason":     session.PageStopReason,
	}
	log.Info().
		Str("component", componentName).
		Int("page_count", session.PageCount).
		Str("reason", session.PageStopReason).
		Msg("warehouse page finish branch matched")
	return customRecognitionResult(arg, detail)
}

// scrollProbeUnchangedResult matches when the post-scroll top row is still mostly the previous page.
func scrollProbeUnchangedResult(arg *maa.CustomRecognitionArg, session *runSession) (*maa.CustomRecognitionResult, bool) {
	unchanged, comparable, matches := scrollProbeUnchanged(session.ScanConfig.Probe, session.LastHeadProbe, session.CurrentProbe)
	reason := "scroll probe changed"
	if unchanged {
		reason = "warehouse scan reached bottom / probe mostly unchanged"
	}
	log.Info().
		Str("component", componentName).
		Int("comparable", comparable).
		Int("matches", matches).
		Ints("mismatch_cells", probeMismatchCells(session.LastHeadProbe, session.CurrentProbe)).
		Float64("match_ratio", matchRatio(comparable, matches)).
		Float64("min_match_ratio", session.ScanConfig.Probe.MinMatchRatio).
		Int("max_mismatches", session.ScanConfig.Probe.MaxMismatches).
		Interface("before_probe", session.LastHeadProbe).
		Interface("after_probe", session.CurrentProbe).
		Bool("unchanged", unchanged).
		Msg(reason)
	if !unchanged {
		return nil, false
	}
	return customRecognitionResult(arg, map[string]any{
		"stage":      stageScrollProbeUnchanged,
		"comparable": comparable,
		"matches":    matches,
		"reason":     reason,
	})
}

// customRecognitionResult returns a DirectHit-like custom result with diagnostic detail JSON.
func customRecognitionResult(arg *maa.CustomRecognitionArg, detail map[string]any) (*maa.CustomRecognitionResult, bool) {
	raw, err := json.Marshal(detail)
	if err != nil {
		log.Error().Err(err).Str("component", componentName).Msg("failed to marshal custom recognition detail")
		return nil, false
	}
	return &maa.CustomRecognitionResult{
		Box:    arg.Roi,
		Detail: string(raw),
	}, true
}
