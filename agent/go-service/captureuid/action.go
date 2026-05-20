package captureuid

import (
	"encoding/json"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

type captureUidParam struct {
	UseCache            *bool `json:"use_cache,omitempty"`
	StayOnCurrentScreen *bool `json:"stay_on_current_screen,omitempty"`
	AllowUnknown        *bool `json:"allow_unknown,omitempty"`
}

type CaptureUidAction struct{}

var _ maa.CustomActionRunner = &CaptureUidAction{}

func (a *CaptureUidAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if ctx == nil || ctx.GetTasker() == nil {
		log.Error().Str("component", component).Msg("CaptureUid: nil context or tasker")
		return false
	}

	ctrl := ctx.GetTasker().GetController()
	if ctrl == nil {
		log.Error().Str("component", component).Msg("CaptureUid: nil controller")
		return false
	}

	useCache := true
	stayOnCurrentScreen := true
	allowUnknown := true

	if arg != nil && arg.CustomActionParam != "" {
		var params captureUidParam
		if err := json.Unmarshal([]byte(arg.CustomActionParam), &params); err != nil {
			log.Error().Err(err).Str("component", component).Str("param", arg.CustomActionParam).
				Msg("CaptureUid: failed to parse custom_action_param")
			return false
		}
		if params.UseCache != nil {
			useCache = *params.UseCache
		}
		if params.StayOnCurrentScreen != nil {
			stayOnCurrentScreen = *params.StayOnCurrentScreen
		}
		if params.AllowUnknown != nil {
			allowUnknown = *params.AllowUnknown
		}
	}

	uid, err := Capture(ctx, ctrl, useCache, stayOnCurrentScreen, allowUnknown)
	if err != nil {
		log.Error().Err(err).Str("component", component).Msg("CaptureUid: capture failed")
		return false
	}

	log.Info().Str("component", component).Str("uid", uid).
		Bool("use_cache", useCache).
		Bool("stay_on_current_screen", stayOnCurrentScreen).
		Bool("allow_unknown", allowUnknown).
		Msg("CaptureUid: done")
	return true
}
