package aspectratio

import (
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/control"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/maafocus"
	"github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const (
	// Target aspect ratio: 16:9
	targetRatio = 16.0 / 9.0
	// Tolerance for aspect ratio comparison (±2%)
	tolerance    = 0.02
	targetWidth  = 1280
	targetHeight = 720
)

// AspectRatioChecker checks if the device resolution is 16:9 before task execution
type AspectRatioChecker struct {
	mu              sync.Mutex
	pendingWarnings map[uint64]string
}

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

	const maxRetries = 20
	var width, height int32
	var err error
	for i := range maxRetries {
		width, height, err = controller.GetResolution()
		if err != nil {
			log.Error().Err(err).Msg("Failed to get resolution")
			return
		}
		if width > 100 && height > 100 {
			break
		}
		log.Debug().
			Int32("width", width).
			Int32("height", height).
			Int("attempt", i+1).
			Msg("Resolution too small, window may not be ready yet, retrying...")
		time.Sleep(time.Second)
		controller.PostScreencap().Wait()
	}

	if width <= 100 || height <= 100 {
		log.Error().
			Int32("width", width).
			Int32("height", height).
			Msg("Resolution still too small after max retries, skipping aspect ratio check")
		return
	}

	log.Debug().
		Int32("width", width).
		Int32("height", height).
		Msg("Got resolution")

	isADBController := false
	controlType := "unknown"
	controllerTypeSource := "fallback"
	if control.CachedControlType != "" {
		controlType = control.CachedControlType
		controllerTypeSource = "cache"
	} else {
		controllerTypeSource = "controller_info"
	}

	controlType, controlErr := resolveControllerType(controller, controlType)
	if controlErr != nil {
		log.Warn().
			Err(controlErr).
			Uint64("task_id", detail.TaskID).
			Str("entry", detail.Entry).
			Int32("width", width).
			Int32("height", height).
			Msg("Failed to detect controller type, falling back to aspect ratio check")
	} else {
		isADBController = controlType == control.CONTROL_TYPE_ADB
		log.Debug().
			Uint64("task_id", detail.TaskID).
			Str("entry", detail.Entry).
			Str("controller_type", controlType).
			Str("controller_type_source", controllerTypeSource).
			Bool("is_adb_controller", isADBController).
			Int32("width", width).
			Int32("height", height).
			Msg("Detected controller type for aspect ratio check")
	}

	if isADBController {
		requirement := exactResolutionRequirement()
		log.Debug().
			Uint64("task_id", detail.TaskID).
			Str("entry", detail.Entry).
			Str("controller_type", controlType).
			Str("requirement", "exact_resolution").
			Str("target_resolution", requirement).
			Str("mode", "adb_exact_resolution").
			Int32("width", width).
			Int32("height", height).
			Int("target_width", targetWidth).
			Int("target_height", targetHeight).
			Msg("Using exact resolution check for ADB controller")

		if int(width) == targetWidth && int(height) == targetHeight {
			log.Debug().
				Uint64("task_id", detail.TaskID).
				Str("entry", detail.Entry).
				Str("controller_type", controlType).
				Str("requirement", "exact_resolution").
				Str("target_resolution", requirement).
				Int32("width", width).
				Int32("height", height).
				Str("mode", "adb_exact_resolution").
				Msg("resolution check passed")
			return
		}

		log.Error().
			Uint64("task_id", detail.TaskID).
			Str("entry", detail.Entry).
			Str("controller_type", controlType).
			Str("requirement", "exact_resolution").
			Str("target_resolution", requirement).
			Bool("stop_task", true).
			Int32("width", width).
			Int32("height", height).
			Int("target_width", targetWidth).
			Int("target_height", targetHeight).
			Str("mode", "adb_exact_resolution").
			Msg("resolution check failed")
		warning := i18n.RenderHTML("tasker.aspect_ratio_warning", buildWarningData(controlType, int(width), int(height), requirement))
		c.scheduleWarning(detail.TaskID, warning)
		return
	}

	requirement := aspectRatioRequirement()
	log.Debug().
		Uint64("task_id", detail.TaskID).
		Str("entry", detail.Entry).
		Str("controller_type", controlType).
		Str("requirement", "aspect_ratio").
		Str("target_resolution", requirement).
		Str("mode", "aspect_ratio_only").
		Int32("width", width).
		Int32("height", height).
		Float64("target_ratio", targetRatio).
		Msg("Using aspect ratio check for non-ADB controller")

	if !isAspectRatio16x9(int(width), int(height)) {
		actualRatio := calculateAspectRatio(int(width), int(height))
		log.Error().
			Uint64("task_id", detail.TaskID).
			Str("entry", detail.Entry).
			Str("controller_type", controlType).
			Str("requirement", "aspect_ratio").
			Str("target_resolution", requirement).
			Bool("stop_task", true).
			Int32("width", width).
			Int32("height", height).
			Float64("actual_ratio", actualRatio).
			Float64("target_ratio", targetRatio).
			Str("mode", "aspect_ratio_only").
			Msg("resolution check failed")
		warning := i18n.RenderHTML("tasker.aspect_ratio_warning", buildWarningData(controlType, int(width), int(height), requirement))
		c.scheduleWarning(detail.TaskID, warning)
		return
	}

	log.Debug().
		Uint64("task_id", detail.TaskID).
		Str("entry", detail.Entry).
		Str("controller_type", controlType).
		Str("requirement", "aspect_ratio").
		Str("target_resolution", requirement).
		Int32("width", width).
		Int32("height", height).
		Str("mode", "aspect_ratio_only").
		Msg("resolution check passed")
}

func (c *AspectRatioChecker) scheduleWarning(taskID uint64, content string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.pendingWarnings == nil {
		c.pendingWarnings = make(map[uint64]string)
	}
	c.pendingWarnings[taskID] = content
	log.Debug().
		Uint64("task_id", taskID).
		Msg("scheduled focus warning for resolution check failure")
}

func (c *AspectRatioChecker) consumeWarning(taskID uint64) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.pendingWarnings == nil {
		return "", false
	}
	content, ok := c.pendingWarnings[taskID]
	if ok {
		delete(c.pendingWarnings, taskID)
	}
	return content, ok
}

func (c *AspectRatioChecker) OnNodePipelineNode(ctx *maa.Context, event maa.EventStatus, detail maa.NodePipelineNodeDetail) {
	if event != maa.EventStatusStarting {
		return
	}

	content, ok := c.consumeWarning(detail.TaskID)
	if !ok {
		return
	}

	log.Debug().
		Uint64("task_id", detail.TaskID).
		Str("node", detail.Name).
		Msg("sending focus warning for resolution check failure")
	maafocus.Print(ctx, content)
	ctx.GetTasker().PostStop()
}

func (c *AspectRatioChecker) OnNodeRecognitionNode(_ *maa.Context, _ maa.EventStatus, _ maa.NodeRecognitionNodeDetail) {
}

func (c *AspectRatioChecker) OnNodeActionNode(_ *maa.Context, _ maa.EventStatus, _ maa.NodeActionNodeDetail) {
}

func (c *AspectRatioChecker) OnNodeNextList(_ *maa.Context, _ maa.EventStatus, _ maa.NodeNextListDetail) {
}

func (c *AspectRatioChecker) OnNodeRecognition(_ *maa.Context, _ maa.EventStatus, _ maa.NodeRecognitionDetail) {
}

func (c *AspectRatioChecker) OnNodeAction(_ *maa.Context, _ maa.EventStatus, _ maa.NodeActionDetail) {
}

func resolveControllerType(controller *maa.Controller, cachedType string) (string, error) {
	if cachedType != "" {
		return cachedType, nil
	}
	return control.GetControlType(controller)
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

func buildWarningData(controllerType string, width, height int, requirement string) map[string]any {
	return map[string]any{
		"ControllerType":    displayControllerType(controllerType),
		"CurrentResolution": fmt.Sprintf("%dx%d", width, height),
		"Requirement":       requirement,
	}
}

func displayControllerType(controllerType string) string {
	switch controllerType {
	case control.CONTROL_TYPE_ADB:
		return "ADB"
	case control.CONTROL_TYPE_WIN32:
		return "Win32"
	default:
		return controllerType
	}
}

func exactResolutionRequirement() string {
	return fmt.Sprintf("%dx%d", targetWidth, targetHeight)
}

func aspectRatioRequirement() string {
	return "16:9"
}
