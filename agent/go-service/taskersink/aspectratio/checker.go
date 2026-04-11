package aspectratio

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/control"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/pienv"
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
	controlType, controllerTypeSource, controlErr := resolveControllerType(controller)
	controllerDisplay := displayController(pienv.ControllerName(), controlType)
	if controlErr != nil {
		log.Warn().
			Err(controlErr).
			Uint64("task_id", detail.TaskID).
			Str("entry", detail.Entry).
			Str("controller_name", pienv.ControllerName()).
			Str("controller_type_from_pi", pienv.ControllerType()).
			Int32("width", width).
			Int32("height", height).
			Msg("Failed to detect controller type, falling back to aspect ratio check")
	} else {
		isADBController = controlType == control.CONTROL_TYPE_ADB
		log.Debug().
			Uint64("task_id", detail.TaskID).
			Str("entry", detail.Entry).
			Str("controller_name", pienv.ControllerName()).
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
			Str("controller_name", pienv.ControllerName()).
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
				Str("controller_name", pienv.ControllerName()).
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
			Str("controller_name", pienv.ControllerName()).
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
		c.stopWithWarning(tasker, controllerDisplay, int(width), int(height), requirement)
		return
	}

	requirement := aspectRatioRequirement()
	log.Debug().
		Uint64("task_id", detail.TaskID).
		Str("entry", detail.Entry).
		Str("controller_name", pienv.ControllerName()).
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
			Str("controller_name", pienv.ControllerName()).
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
		c.stopWithWarning(tasker, controllerDisplay, int(width), int(height), requirement)
		return
	}

	log.Debug().
		Uint64("task_id", detail.TaskID).
		Str("entry", detail.Entry).
		Str("controller_name", pienv.ControllerName()).
		Str("controller_type", controlType).
		Str("requirement", "aspect_ratio").
		Str("target_resolution", requirement).
		Int32("width", width).
		Int32("height", height).
		Str("mode", "aspect_ratio_only").
		Msg("resolution check passed")
}

func (c *AspectRatioChecker) stopWithWarning(tasker *maa.Tasker, controllerDisplay string, width, height int, requirement string) {
	content := i18n.RenderHTML("tasker.aspect_ratio_warning", buildWarningData(controllerDisplay, width, height, requirement))
	fmt.Println(content)
	tasker.PostStop()
}

func resolveControllerType(controller *maa.Controller) (string, string, error) {
	if controlType := normalizeControllerType(pienv.ControllerType()); controlType != "" {
		return controlType, "pi_env", nil
	}
	if controlType := normalizeControllerType(control.CachedControlType); controlType != "" {
		return controlType, "cache", nil
	}

	controlType, err := control.GetControlType(controller)
	if err != nil {
		return "unknown", "controller_info", err
	}

	if normalized := normalizeControllerType(controlType); normalized != "" {
		return normalized, "controller_info", nil
	}
	return "unknown", "controller_info", nil
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

func buildWarningData(controllerDisplay string, width, height int, requirement string) map[string]any {
	return map[string]any{
		"ControllerType":    controllerDisplay,
		"CurrentResolution": fmt.Sprintf("%dx%d", width, height),
		"Requirement":       requirement,
	}
}

func displayController(name, controllerType string) string {
	typeLabel := displayControllerType(controllerType)
	if name == "" {
		if typeLabel == "" {
			return "unknown"
		}
		return typeLabel
	}
	if typeLabel == "" || strings.EqualFold(name, typeLabel) {
		return name
	}
	return fmt.Sprintf("%s (%s)", name, typeLabel)
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

func normalizeControllerType(controllerType string) string {
	switch strings.ToLower(strings.TrimSpace(controllerType)) {
	case "adb":
		return control.CONTROL_TYPE_ADB
	case "win32":
		return control.CONTROL_TYPE_WIN32
	default:
		return ""
	}
}

func exactResolutionRequirement() string {
	return i18n.T("tasker.aspect_ratio_warning.requirement_exact", targetWidth, targetHeight)
}

func aspectRatioRequirement() string {
	return i18n.T("tasker.aspect_ratio_warning.requirement_ratio")
}
