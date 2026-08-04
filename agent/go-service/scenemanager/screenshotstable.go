package scenemanager

import (
	"image"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/minicv"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

var screenshotStableDone bool
var screenshotStableCount int // 已采样帧数：0→存 baseline；1/2→与 baseline 比较
var screenshotStableBaseline *image.RGBA

var _ maa.CustomRecognitionRunner = (*ScreenshotStableRecognition)(nil)

// ScreenshotStableRecognition 在 Agent 进程生命周期内只判定一次画面是否连续三帧稳定。
// 每次调用只用 arg.Img（固定全屏）：第 1 次把 baseline 留在内存；第 2/3 次与 baseline 做 NCC
//（阈值 0.99）；仅第 3 次仍相似时命中；周期结束后恒未命中。不落盘、不 OverrideImage。
type ScreenshotStableRecognition struct{}

func (r *ScreenshotStableRecognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	_ = ctx
	if arg == nil || arg.Img == nil {
		log.Error().Str("component", "ScreenshotStableRecognition").Msg("nil arg or image")
		return nil, false
	}
	if screenshotStableDone {
		return nil, false
	}

	cur := minicv.ImageConvertRGBA(arg.Img)
	if cur == nil || cur.Bounds().Empty() {
		log.Error().Str("component", "ScreenshotStableRecognition").Msg("empty image")
		return nil, false
	}

	if screenshotStableCount == 0 {
		screenshotStableBaseline = minicv.ImageCopy(cur)
		screenshotStableCount = 1
		return nil, false
	}

	matched := imagesSimilar(cur, screenshotStableBaseline, 0.99)
	screenshotStableCount++

	if !matched || screenshotStableCount >= 3 {
		screenshotStableDone = true
		screenshotStableBaseline = nil
	}
	if !matched || screenshotStableCount < 3 {
		return nil, false
	}

	b := cur.Bounds()
	return &maa.CustomRecognitionResult{
		Box:    maa.Rect{b.Min.X, b.Min.Y, b.Dx(), b.Dy()},
		Detail: `{"stable":true}`,
	}, true
}

func imagesSimilar(cur, baseline *image.RGBA, threshold float64) bool {
	if cur == nil || baseline == nil {
		return false
	}
	if cur.Bounds().Dx() != baseline.Bounds().Dx() || cur.Bounds().Dy() != baseline.Bounds().Dy() {
		return false
	}
	stats := minicv.GetImageStats(baseline)
	if stats.Std < 1e-6 {
		return false
	}
	score := minicv.ComputeNCC(cur, minicv.GetIntegralArray(cur), baseline, stats, 0, 0)
	return score >= threshold
}
