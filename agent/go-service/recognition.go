package main

import (
	"github.com/MaaXYZ/maa-framework-go/v3"
	"github.com/rs/zerolog/log"
)

// myRecognition implements a simple custom recognition that always succeeds.
type myRecognition struct{}

func (r *myRecognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	log.Debug().
		Str("recognition", arg.CustomRecognitionName).
		Str("task", arg.CurrentTaskName).
		Str("param", arg.CustomRecognitionParam).
		Msg("Running recognition")

	// Return a result with the ROI as the detected box
	result := &maa.CustomRecognitionResult{
		Box:    arg.Roi,
		Detail: `{"custom": "recognition result"}`,
	}
	return result, true
}
