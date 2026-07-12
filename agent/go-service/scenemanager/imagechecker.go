package scenemanager

import (
	"encoding/json"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

var imageCheckPass = false

var _ maa.CustomRecognitionRunner = (*ImageCheckNotPassedRecognition)(nil)

type ImageCheckNotPassedRecognition struct{}

func (r *ImageCheckNotPassedRecognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	_ = ctx
	if arg == nil {
		log.Error().Str("component", "ImageCheckNotPassedRecognition").Msg("nil recognition arg")
		return nil, false
	}

	if imageCheckPass {
		return nil, false
	}

	return &maa.CustomRecognitionResult{
		Box:    arg.Roi,
		Detail: `{"passed":false}`,
	}, true
}

var _ maa.CustomActionRunner = (*ImageCheckSetResultAction)(nil)

type imageCheckSetResultParam struct {
	Result bool `json:"result"`
}

// ImageCheckSetResultAction 设置是否检查通过（true 则 ImageCheckNotPassedRecognition 不再命中）。
type ImageCheckSetResultAction struct{}

func (a *ImageCheckSetResultAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	_ = ctx
	if arg == nil {
		log.Error().Str("component", "ImageCheckSetResultAction").Msg("nil custom action arg")
		return false
	}

	var params imageCheckSetResultParam
	if err := json.Unmarshal([]byte(arg.CustomActionParam), &params); err != nil {
		log.Error().
			Err(err).
			Str("component", "ImageCheckSetResultAction").
			Str("custom_action_param", arg.CustomActionParam).
			Msg("failed to parse param")
		return false
	}

	imageCheckPass = params.Result
	log.Debug().
		Str("component", "ImageCheckSetResultAction").
		Bool("result", params.Result).
		Msg("image check result set")
	return true
}
