package shakecamera

import (
	"encoding/json"
	"time"

	"github.com/rs/zerolog/log"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

type shakeCameraParam struct {
	Intensity int `json:"intensity"`
	Count     int `json:"count"`
}

type ShakeCameraAction struct{}

var _ maa.CustomActionRunner = &ShakeCameraAction{}

func (a *ShakeCameraAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if arg == nil || ctx == nil {
		log.Warn().Msg("ShakeCamera: nil arg or context")
		return false
	}

	params := shakeCameraParam{Intensity: 5, Count: 6}
	if arg.CustomActionParam != "" {
		if err := json.Unmarshal([]byte(arg.CustomActionParam), &params); err != nil {
			log.Warn().Err(err).Msg("ShakeCamera: parse param failed, using defaults")
		}
	}
	if params.Intensity <= 0 {
		params.Intensity = 5
	}
	if params.Count <= 0 {
		params.Count = 6
	}

	ctrl := ctx.GetTasker().GetController()
	if ctrl == nil {
		log.Warn().Msg("ShakeCamera: controller is nil")
		return false
	}

	for i := 0; i < params.Count; i++ {
		dx := int32(params.Intensity)
		if i%2 == 0 {
			dx = -dx
		}
		ctrl.PostRelativeMove(dx, 0).Wait()
		time.Sleep(30 * time.Millisecond)
	}

	return true
}
