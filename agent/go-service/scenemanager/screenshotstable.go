package scenemanager

import (
	"image"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/minicv"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/pienv"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pretask/gamesetting"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

var screenshotStableDone bool
var screenshotStableCount int // 0=未采；1=已有 baseline；2=第 2 帧已匹配，等待第 3 帧
var screenshotStableBaseline *image.RGBA

var _ maa.CustomRecognitionRunner = (*ScreenshotStableRecognition)(nil)

// ScreenshotStableRecognition 仅在 Win32-Front + 游戏全屏时，于 Agent 进程生命周期内判定一次
// 画面是否连续三帧稳定。每次调用只用 arg.Img：第 1 次存 baseline；第 2 帧不匹配则结束周期；
// 匹配后再采第 3 帧确认。仅第 3 帧仍相似时命中。
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
	if !shouldRunScreenshotStable() {
		return nil, false
	}

	cur := minicv.ImageConvertRGBA(arg.Img)
	if cur == nil || cur.Bounds().Empty() {
		log.Error().Str("component", "ScreenshotStableRecognition").Msg("empty image")
		return nil, false
	}

	// 第 1 帧：只存 baseline
	if screenshotStableCount == 0 {
		screenshotStableBaseline = minicv.ImageCopy(cur)
		screenshotStableCount = 1
		return nil, false
	}

	// 第 2 帧：与 baseline 比；不匹配则结束，不再需要第 3 帧
	if screenshotStableCount == 1 {
		if !imagesSimilar(cur, screenshotStableBaseline, 0.99) {
			finishScreenshotStable()
			return nil, false
		}
		screenshotStableCount = 2
		return nil, false
	}

	// 第 3 帧：仅在第 2 帧已匹配时才会走到这里
	matched := imagesSimilar(cur, screenshotStableBaseline, 0.99)
	finishScreenshotStable()
	if !matched {
		return nil, false
	}

	b := cur.Bounds()
	return &maa.CustomRecognitionResult{
		Box:    maa.Rect{b.Min.X, b.Min.Y, b.Dx(), b.Dy()},
		Detail: `{"stable":true}`,
	}, true
}

func shouldRunScreenshotStable() bool {
	if pienv.ControllerName() != "Win32-Front" {
		return false
	}
	fullScreen, err := gamesetting.GetVideoFullScreen()
	if err != nil {
		log.Debug().
			Err(err).
			Str("component", "ScreenshotStableRecognition").
			Msg("failed to read fullscreen setting, skip")
		return false
	}
	return fullScreen == 1
}

func finishScreenshotStable() {
	screenshotStableDone = true
	screenshotStableBaseline = nil
	screenshotStableCount = 0
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
