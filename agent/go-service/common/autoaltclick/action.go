package autoaltclick

import (
	"encoding/json"
	"time"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/control"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

type autoAltLongPressParam struct {
	Duration int64 `json:"duration"`
}

type autoAltClickParam struct {
	ToggleMouseLock  bool `json:"toggle_mouse_lock"`
	RestoreMouseLock bool `json:"restore_mouse_lock"`
}

const autoAltClickMouseLockDelay = 200 * time.Millisecond

type AutoAltClickAction struct{}

// Compile-time interface check
var _ maa.CustomActionRunner = &AutoAltClickAction{}

func (a *AutoAltClickAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	var p autoAltClickParam
	if arg.CustomActionParam != "" {
		if err := json.Unmarshal([]byte(arg.CustomActionParam), &p); err != nil {
			log.Error().
				Err(err).
				Str("component", "AutoAltClickAction").
				Str("custom_action_param", arg.CustomActionParam).
				Msg("failed to parse custom action param")
			return false
		}
	}

	ctrl := ctx.GetTasker().GetController()
	toggleMouseLock := p.ToggleMouseLock && control.IsMessageInputWin32(ctrl)
	if toggleMouseLock {
		control.TrySetMouseLockFollow(ctrl, false)
		time.Sleep(autoAltClickMouseLockDelay)
	}
	ctx.RunAction("__AutoAltClickAltKeyDownAction",
		maa.Rect{0, 0, 0, 0}, "", nil)
	ctx.RunAction("__AutoAltClickMouseClickAction",
		arg.Box, "", nil)
	if toggleMouseLock && p.RestoreMouseLock {
		control.TrySetMouseLockFollow(ctrl, true)
		time.Sleep(autoAltClickMouseLockDelay)
	}
	ctx.RunAction("__AutoAltClickAltKeyUpAction",
		maa.Rect{0, 0, 0, 0}, "", nil)
	return true
}

type AutoAltLongPressAction struct{}

var _ maa.CustomActionRunner = &AutoAltLongPressAction{}

func (a *AutoAltLongPressAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	var p autoAltLongPressParam
	if err := json.Unmarshal([]byte(arg.CustomActionParam), &p); err != nil {
		log.Error().
			Err(err).
			Str("component", "AutoAltLongPressAction").
			Str("custom_action_param", arg.CustomActionParam).
			Msg("failed to parse custom action param")
		return false
	}
	if p.Duration <= 0 {
		log.Error().
			Str("component", "AutoAltLongPressAction").
			Int64("duration", p.Duration).
			Msg("duration must be greater than 0")
		return false
	}

	if _, err := ctx.RunAction("__AutoAltClickAltKeyDownAction",
		maa.Rect{0, 0, 0, 0}, "", nil); err != nil {
		log.Error().
			Err(err).
			Str("component", "AutoAltLongPressAction").
			Msg("failed to run __AutoAltClickAltKeyDownAction")
		return false
	}

	_, longPressErr := ctx.RunAction("__AutoAltLongPressMouseLongPressAction",
		arg.Box, "", map[string]any{
			"__AutoAltLongPressMouseLongPressAction": map[string]any{
				"duration": p.Duration,
			},
		})
	if longPressErr != nil {
		log.Error().
			Err(longPressErr).
			Str("component", "AutoAltLongPressAction").
			Msg("failed to run __AutoAltLongPressMouseLongPressAction")
	}

	if _, err := ctx.RunAction("__AutoAltClickAltKeyUpAction",
		maa.Rect{0, 0, 0, 0}, "", nil); err != nil {
		log.Error().
			Err(err).
			Str("component", "AutoAltLongPressAction").
			Msg("failed to run __AutoAltClickAltKeyUpAction")
	}

	return longPressErr == nil
}
