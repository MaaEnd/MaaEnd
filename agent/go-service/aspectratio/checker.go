package aspectratio

import (
	_ "embed"
	"fmt"
	"math"

	"github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const (
	// Target aspect ratio: 16:9
	targetRatio = 16.0 / 9.0
	// Tolerance for aspect ratio comparison (±2%)
	tolerance = 0.02
)

//go:embed warning_message.html
var aspectRatioWarningHTML string

// AspectRatioChecker checks if the device resolution is 16:9 before task execution
type AspectRatioChecker struct{}

// OnTaskerTask handles tasker task events
func (c *AspectRatioChecker) OnTaskerTask(tasker *maa.Tasker, event maa.EventStatus, detail maa.TaskerTaskDetail) {
	// Only check on task starting
	if event != maa.EventStatusStarting {
		return
	}
	
	if detail.Entry == "MaaTaskerPostStop" {
		// Ignore post-stop events to avoid redundant checks
		log.Debug().Msg("Received PostStop event, skipping aspect ratio check")
		return
	}
	
	log.Debug().
		Uint64("task_id", detail.TaskID).
		Str("entry", detail.Entry).
		Msg("Checking aspect ratio before task execution")

	// Get controller from tasker
	controller := tasker.GetController()
	if controller == nil {
		log.Error().Msg("Failed to get controller from tasker")
		return
	}

	// Get the cached image
	img, err := controller.CacheImage()
	if err != nil {
		log.Error().
			Err(err).
			Msg("Failed to get cached image")
		return
	}
	if img == nil {
		log.Error().Msg("Failed to get cached image")
		return
	}

	// Get image dimensions
	bounds := img.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()

	log.Debug().
		Int("width", width).
		Int("height", height).
		Msg("Got screenshot dimensions")

	// Check aspect ratio
	if !isAspectRatio16x9(width, height) {
		actualRatio := calculateAspectRatio(width, height)
		log.Error().
			Int("width", width).
			Int("height", height).
			Float64("actual_ratio", actualRatio).
			Float64("target_ratio", targetRatio).
			Msg("Resolution is not 16:9! Task will be stopped.")
		fmt.Println(aspectRatioWarningHTML)

		// Stop the task
		tasker.PostStop()
	} else {
		log.Debug().
			Int("width", width).
			Int("height", height).
			Msg("Resolution check passed: 16:9")
	}
}

// isAspectRatio16x9 checks if the given dimensions are approximately 16:9
// This handles both landscape (16:9) and portrait (9:16) orientations
func isAspectRatio16x9(width, height int) bool {
	if width <= 0 || height <= 0 {
		return false
	}

	ratio := calculateAspectRatio(width, height)

	// Check if ratio is within tolerance of 16:9
	return math.Abs(ratio-targetRatio) <= targetRatio*tolerance
}

// calculateAspectRatio calculates the aspect ratio, always returning the larger/smaller ratio
// This normalizes both landscape and portrait orientations
func calculateAspectRatio(width, height int) float64 {
	w := float64(width)
	h := float64(height)

	// Always return wider/narrower to normalize orientation
	if w > h {
		return w / h
	}
	return h / w
}
