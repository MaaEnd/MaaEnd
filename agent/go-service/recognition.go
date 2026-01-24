package main

import (
	"log/slog"

	"github.com/MaaXYZ/maa-framework-go/v3"
)

// myRecognition implements a simple custom recognition that always succeeds.
type myRecognition struct{}

func (r *myRecognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	slog.Debug("Running recognition",
		"recognition", arg.CustomRecognitionName,
		"task", arg.CurrentTaskName,
		"param", arg.CustomRecognitionParam,
	)

	// Return a result with the ROI as the detected box
	result := &maa.CustomRecognitionResult{
		Box:    arg.Roi,
		Detail: `{"custom": "recognition result"}`,
	}
	return result, true
}
