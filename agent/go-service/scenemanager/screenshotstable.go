package scenemanager

import (
	"image"
	"time"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/minicv"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/pienv"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

var screenshotStableDone bool
var screenshotStableCount int // 0=未采；1=已有 baseline；2=第 2 帧已匹配，等待第 3 帧
var screenshotStableBaseline *image.RGBA
var screenshotStableBaselineAt time.Time

var _ maa.CustomRecognitionRunner = (*ScreenshotStableRecognition)(nil)

// ScreenshotStableRecognition 仅在 Win32-Front 时，于 Agent 进程生命周期内判定一次
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

	// 中断恢复：周期进行中若长时间没有后续帧（任务被打断），丢弃 baseline
	// 重新起算，避免基准帧（约 3.5MB）驻留到进程结束。
	if screenshotStableCount != 0 && time.Since(screenshotStableBaselineAt) > 10*time.Second {
		restartScreenshotStable()
	}

	cur := minicv.ImageConvertRGBA(arg.Img)
	if cur == nil || cur.Bounds().Empty() {
		log.Error().Str("component", "ScreenshotStableRecognition").Msg("empty image")
		return nil, false
	}

	// 第 1 帧：只存 baseline
	if screenshotStableCount == 0 {
		screenshotStableBaseline = minicv.ImageCopy(cur)
		screenshotStableBaselineAt = time.Now()
		screenshotStableCount = 1
		return nil, false
	}

	// 第 2 帧：与 baseline 比；不匹配则结束，不再需要第 3 帧
	if screenshotStableCount == 1 {
		if !imagesSimilar(cur, screenshotStableBaseline, 0.99) {
			finishScreenshotStable()
			return nil, false
		}
		// 以最近一帧为基准刷新中断计时：否则 0/6/12s 这种稀疏序列会在第 3 帧
		// 被误判为"中断恢复"而重启，稳定判定永远凑不满 3 帧。
		screenshotStableBaselineAt = time.Now()
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
	return pienv.ControllerName() == "Win32-Front"
}

func finishScreenshotStable() {
	screenshotStableDone = true
	restartScreenshotStable()
}

// restartScreenshotStable 丢弃进行中的周期状态（不动 Done 标志）。
func restartScreenshotStable() {
	screenshotStableBaseline = nil
	screenshotStableBaselineAt = time.Time{}
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
