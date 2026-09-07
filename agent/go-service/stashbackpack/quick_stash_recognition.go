package stashbackpack

import maa "github.com/MaaXYZ/maa-framework-go/v4"

// QuickStashEnabledRecognition 将本批次存放设置提供给后续嵌入任务。
type QuickStashEnabledRecognition struct{}

var _ maa.CustomRecognitionRunner = &QuickStashEnabledRecognition{}

// Run 仅在前置存放成功且开启了一键存放时命中，实际点击仍由 Pipeline 完成。
func (r *QuickStashEnabledRecognition) Run(_ *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	if arg == nil || !globalState.quickStashEnabled() {
		return nil, false
	}
	return &maa.CustomRecognitionResult{Box: arg.Roi}, true
}
