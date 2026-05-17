// Copyright (c) 2026 Harry Huang
package webevent202605

import (
	"encoding/json"
	"fmt"
	"image"
	"math"
	"time"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/maafocus"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

var _ maa.CustomActionRunner = &WebEvent202605Action{}

type WebEvent202605Action struct{}

type webEvent202605Param struct {
	MaxShot int `json:"max_shot"`
}

func (a *WebEvent202605Action) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	tasker := ctx.GetTasker()
	if tasker == nil {
		log.Error().Str("component", "WebEvent202605").Msg("tasker is nil")
		return false
	}

	controller := tasker.GetController()
	if controller == nil {
		log.Error().Str("component", "WebEvent202605").Msg("controller is nil")
		return false
	}

	const (
		latencySamples   = 10
		speedObserveSec  = 3
		predictTolerance = 0.0625
		defaultMaxShot   = 25
		timeoutLv1Sec    = 30
		timeoutLv2Sec    = 5
	)
	var total time.Duration

	// Parse parameters
	maxShot := defaultMaxShot
	if arg != nil && arg.CustomActionParam != "" {
		var param webEvent202605Param
		if err := json.Unmarshal([]byte(arg.CustomActionParam), &param); err != nil {
			log.Error().Err(err).Str("component", "WebEvent202605").Msg("failed to parse custom_action_param")
			return false
		}
		if param.MaxShot > 0 {
			maxShot = param.MaxShot
		}
	}
	maafocus.Print(ctx, "正在估计环境参数...")

	// Estimate screen operations latency
	for i := 0; i < latencySamples; i++ {
		if tasker.Stopping() {
			controller.PostTouchUp(0).Wait()
			log.Warn().Str("component", "WebEvent202605").Msg("task stopping, exiting")
			return false
		}

		start := time.Now()
		controller.PostScreencap().Wait()
		img, err := controller.CacheImage()
		if err != nil {
			log.Error().Err(err).Str("component", "WebEvent202605").Int("index", i).Msg("failed to cache image")
			return false
		}
		if img == nil {
			log.Error().Str("component", "WebEvent202605").Int("index", i).Msg("cached image is nil")
			return false
		}
		_, _ = getPosition(img)
		controller.PostTouchUp(0).Wait()
		total += time.Since(start)
		time.Sleep(time.Duration(100) * time.Millisecond)
	}
	avgLatency := total / time.Duration(latencySamples)
	avgLatencySeconds := avgLatency.Seconds()
	log.Info().
		Str("component", "WebEvent202605").
		Dur("avg_latency", avgLatency).
		Msg("screen operations latency measured")
	maafocus.Print(ctx, fmt.Sprintf("- 截图与响应延迟：%.2fms", avgLatencySeconds*1000))

	// Estimate object speed
	speed, err := getSpeed(ctx, time.Duration(speedObserveSec)*time.Second)
	if err != nil {
		log.Error().Err(err).Str("component", "WebEvent202605").Msg("failed to estimate object speed")
		return false
	}
	log.Info().
		Str("component", "WebEvent202605").
		Float64("max_speed", speed).
		Msg("object speed estimated")
	maafocus.Print(ctx, fmt.Sprintf("- 物体峰值速率：%.2f倍直径/s", speed))
	maafocus.Print(ctx, "环境参数估计完毕，正式开始运行...")

	// Main loop to predict and click
	lastHitAt := time.Now()
	lastEffectivePredictAt := time.Now()
	shots := 0
	for shots < maxShot {
		if tasker.Stopping() {
			controller.PostTouchUp(0).Wait()
			log.Warn().Str("component", "WebEvent202605").Msg("task stopping, exiting")
			return false
		}

		if time.Since(lastHitAt) >= time.Duration(timeoutLv1Sec)*time.Second {
			log.Warn().
				Str("component", "WebEvent202605").
				Int("shots", shots).
				Msg("predict timed out (lv1)")
			return false
		}
		if time.Since(lastEffectivePredictAt) >= time.Duration(timeoutLv2Sec)*time.Second {
			log.Warn().
				Str("component", "WebEvent202605").
				Int("shots", shots).
				Msg("predict timed out (lv2)")
			return false
		}

		hit, err := predict(ctx, avgLatencySeconds, speed, predictTolerance)
		if err != nil {
			hit = false
		} else {
			lastEffectivePredictAt = time.Now()
		}
		if hit {
			img, err := controller.CacheImage()
			if err != nil {
				log.Error().Err(err).Str("component", "WebEvent202605").Msg("failed to cache image for click")
				return false
			}
			if img == nil {
				log.Error().Str("component", "WebEvent202605").Msg("cached image is nil for click")
				return false
			}
			bounds := img.Bounds()
			centerX := bounds.Min.X + bounds.Dx()/2
			centerY := bounds.Min.Y + bounds.Dy()/2
			controller.PostClick(int32(centerX), int32(centerY)).Wait()
			lastHitAt = time.Now()
			shots++
			log.Info().
				Str("component", "WebEvent202605").
				Int("shots", shots).
				Msg("successfully hit once")
			maafocus.Print(ctx, fmt.Sprintf("- 进度：%d/%d", shots, maxShot))
		}
		time.Sleep(time.Duration(50) * time.Millisecond)
	}
	log.Info().Str("component", "WebEvent202605").Msg("max shots reached, exiting normally")

	return true
}

func getPosition(img image.Image) (float64, error) {
	if img == nil {
		return 0, fmt.Errorf("image is nil")
	}

	bounds := img.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()
	if width <= 0 || height <= 0 {
		return 0, fmt.Errorf("invalid image bounds")
	}

	rowY := bounds.Min.Y + int(float64(height)*0.145)
	startX := bounds.Min.X + int(float64(width)*0.378)
	scanWidth := int(float64(width) * 0.244)
	endX := startX + scanWidth - 1

	if scanWidth <= 0 {
		return 0, fmt.Errorf("invalid scan width")
	}
	if rowY < bounds.Min.Y || rowY >= bounds.Max.Y {
		return 0, fmt.Errorf("scan row out of bounds")
	}
	if startX < bounds.Min.X {
		startX = bounds.Min.X
	}
	if endX >= bounds.Max.X {
		endX = bounds.Max.X - 1
	}
	if endX < startX {
		return 0, fmt.Errorf("scan range out of bounds")
	}

	leftX := -1
	for x := startX; x <= endX; x++ {
		r, g, b, _ := img.At(x, rowY).RGBA()
		if int(uint8(r>>8))+int(uint8(g>>8))+int(uint8(b>>8)) >= 255*3-1 {
			leftX = x
			break
		}
	}

	if leftX == -1 {
		return 0, fmt.Errorf("left edge not found")
	}

	rightX := -1
	for x := endX; x >= startX; x-- {
		r, g, b, _ := img.At(x, rowY).RGBA()
		if int(uint8(r>>8))+int(uint8(g>>8))+int(uint8(b>>8)) >= 255*3-1 {
			rightX = x
			break
		}
	}

	if rightX == -1 {
		return 0, fmt.Errorf("right edge not found")
	}

	denom := float64(endX - startX)
	if denom <= 0 {
		return 0, fmt.Errorf("invalid scan range")
	}

	avgX := (float64(leftX) + float64(rightX)) / 2
	position := (avgX - float64(startX)) / denom
	return position, nil
}

func getSpeed(ctx *maa.Context, duration time.Duration) (float64, error) {
	if ctx == nil {
		return 0, fmt.Errorf("context is nil")
	}
	if duration <= 0 {
		return 0, fmt.Errorf("duration must be positive")
	}

	tasker := ctx.GetTasker()
	if tasker == nil {
		return 0, fmt.Errorf("tasker is nil")
	}

	controller := tasker.GetController()
	if controller == nil {
		return 0, fmt.Errorf("controller is nil")
	}

	deadline := time.Now().Add(duration)
	var hasPrev bool
	var prevPos float64
	var prevTime time.Time
	maxSpeed := 0.0
	computed := false

	for time.Now().Before(deadline) {
		controller.PostScreencap().Wait()
		img, err := controller.CacheImage()
		if err != nil || img == nil {
			continue
		}

		pos, err := getPosition(img)
		if err != nil {
			continue
		}

		now := time.Now()
		if hasPrev {
			dt := now.Sub(prevTime).Seconds()
			if dt > 0 {
				speed := math.Abs((pos - prevPos) / dt)
				if !computed || speed > maxSpeed {
					maxSpeed = speed
				}
				computed = true
			}
		}

		prevPos = pos
		prevTime = now
		hasPrev = true
	}

	if !computed {
		return 0, fmt.Errorf("not enough samples to compute speed")
	}
	if maxSpeed < 1e-6 {
		return 0, fmt.Errorf("estimated speed is too small")
	}

	return maxSpeed, nil
}

func predict(ctx *maa.Context, screenLatency float64, speed float64, tolerance float64) (bool, error) {
	if ctx == nil {
		return false, fmt.Errorf("context is nil")
	}
	if screenLatency <= 0 {
		return false, fmt.Errorf("screenLatency must be positive")
	}
	if tolerance < 0 {
		return false, fmt.Errorf("tolerance must be non-negative")
	}

	tasker := ctx.GetTasker()
	if tasker == nil {
		return false, fmt.Errorf("tasker is nil")
	}

	controller := tasker.GetController()
	if controller == nil {
		return false, fmt.Errorf("controller is nil")
	}

	controller.PostScreencap().Wait()
	img, err := controller.CacheImage()
	if err != nil {
		return false, err
	}
	if img == nil {
		return false, fmt.Errorf("cached image is nil")
	}

	pos1, err := getPosition(img)
	if err != nil {
		return false, err
	}

	controller.PostScreencap().Wait()
	img, err = controller.CacheImage()
	if err != nil {
		return false, err
	}
	if img == nil {
		return false, fmt.Errorf("cached image is nil")
	}

	pos2, err := getPosition(img)
	if err != nil {
		return false, err
	}

	delta := pos2 - pos1
	dir := 0.0
	if delta > 0 {
		dir = 1
	} else if delta < 0 {
		// dir = -1
		return false, nil // Only one direction is valid
	}

	predicted := pos2 + dir*math.Abs(speed)*screenLatency
	min := 0.5 - tolerance
	max := 0.5 + tolerance
	if predicted >= min && predicted <= max {
		return true, nil
	}

	return false, nil
}
