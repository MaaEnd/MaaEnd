package iconrecognition

import (
	"fmt"
	"image"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

// Run 调用 IconRecognition Custom Recognition 并解析组件 detail。
func Run(ctx *maa.Context, img image.Image, roi maa.Rect, params Params) (Detail, string, error) {
	if ctx == nil {
		return Detail{}, "", fmt.Errorf("nil context")
	}
	if img == nil {
		return Detail{}, "", fmt.Errorf("nil image")
	}
	detail, err := ctx.RunRecognitionDirect(
		maa.RecognitionTypeCustom,
		&maa.CustomRecognitionParam{
			ROI:                    maa.NewTargetRect(roi),
			CustomRecognition:      CustomRecognitionName,
			CustomRecognitionParam: params,
		},
		img,
	)
	if err != nil {
		return Detail{}, "", fmt.Errorf("run IconRecognition: %w", err)
	}
	return ParseRecognitionDetail(detail)
}
