package aspectratio

import (
	"fmt"
	"math"
	"runtime"
	"strings"
	"time"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pretask/gamesetting"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/control"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/maafocus"
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

type resolutionReader func() (int32, int32, error)

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

	width, height, ok := readResolutionWithRetry(controller)
	if !ok {
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
		requirement := i18n.T("tasker.aspect_ratio_warning.requirement_exact", targetWidth, targetHeight)
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

	aspectRatioOK, minResolutionOK, resolutionOK := isNonADBResolutionOK(width, height)

	log.Debug().
		Uint64("task_id", detail.TaskID).
		Str("entry", detail.Entry).
		Str("controller_name", pienv.ControllerName()).
		Str("controller_type", controlType).
		Str("requirement", "aspect_ratio_min_resolution").
		Str("mode", "aspect_ratio_min_resolution").
		Int32("width", width).
		Int32("height", height).
		Int("target_width", targetWidth).
		Int("target_height", targetHeight).
		Bool("aspect_ratio_ok", aspectRatioOK).
		Bool("min_resolution_ok", minResolutionOK).
		Float64("target_ratio", targetRatio).
		Msg("Using aspect ratio and minimum resolution check for non-ADB controller")

	if !resolutionOK {
		recheckedWidth, recheckedHeight, recheckedOK := trySwitchFullscreenToWindowedAndRecheck(controller, detail, width, height)
		if recheckedOK {
			return
		}
		width = recheckedWidth
		height = recheckedHeight
		aspectRatioOK, minResolutionOK, _ = isNonADBResolutionOK(width, height)
		actualRatio := calculateAspectRatio(int(width), int(height))
		log.Error().
			Uint64("task_id", detail.TaskID).
			Str("entry", detail.Entry).
			Str("controller_name", pienv.ControllerName()).
			Str("controller_type", controlType).
			Str("requirement", "aspect_ratio_min_resolution").
			Bool("stop_task", true).
			Int32("width", width).
			Int32("height", height).
			Int("target_width", targetWidth).
			Int("target_height", targetHeight).
			Bool("aspect_ratio_ok", aspectRatioOK).
			Bool("min_resolution_ok", minResolutionOK).
			Float64("actual_ratio", actualRatio).
			Float64("target_ratio", targetRatio).
			Str("mode", "aspect_ratio_min_resolution").
			Msg("resolution check failed")
		fullScreen, _ := gamesetting.GetVideoFullScreen()
		if fullScreen == 1 {
			c.stopWithWarning(tasker, controllerDisplay, int(width), int(height), i18n.T("tasker.aspect_ratio_warning.full_screen_illegal"))
		} else {
			c.stopWithWarning(tasker, controllerDisplay, int(width), int(height), i18n.T("tasker.aspect_ratio_warning.requirement_ratio"))
		}
		return
	}

	log.Debug().
		Uint64("task_id", detail.TaskID).
		Str("entry", detail.Entry).
		Str("controller_name", pienv.ControllerName()).
		Str("controller_type", controlType).
		Str("requirement", "aspect_ratio_min_resolution").
		Int32("width", width).
		Int32("height", height).
		Int("target_width", targetWidth).
		Int("target_height", targetHeight).
		Bool("aspect_ratio_ok", aspectRatioOK).
		Bool("min_resolution_ok", minResolutionOK).
		Str("mode", "aspect_ratio_min_resolution").
		Msg("resolution check passed")
}

func readResolutionWithRetry(controller *maa.Controller) (int32, int32, bool) {
	const maxRetries = 20
	var width, height int32
	for i := 0; i < maxRetries; i++ {
		var err error
		width, height, err = controller.GetResolution()
		if err != nil {
			log.Error().Err(err).Msg("Failed to get resolution")
			return width, height, false
		}
		if width > 100 && height > 100 {
			return width, height, true
		}
		log.Debug().
			Int32("width", width).
			Int32("height", height).
			Int("attempt", i+1).
			Msg("Resolution too small, window may not be ready yet, retrying...")
		time.Sleep(time.Second)
		controller.PostScreencap().Wait()
	}
	return width, height, false
}

func isNonADBResolutionOK(width, height int32) (bool, bool, bool) {
	aspectRatioOK := isAspectRatio16x9(int(width), int(height))
	minResolutionOK := isAtLeastTargetResolution(int(width), int(height))
	return aspectRatioOK, minResolutionOK, aspectRatioOK && minResolutionOK
}

func trySwitchFullscreenToWindowedAndRecheck(controller *maa.Controller, detail maa.TaskerTaskDetail, width, height int32) (int32, int32, bool) {
	if runtime.GOOS != "windows" {
		log.Debug().
			Uint64("task_id", detail.TaskID).
			Str("entry", detail.Entry).
			Str("goos", runtime.GOOS).
			Msg("Skip Alt+Enter outside Windows")
		return width, height, false
	}

	fullScreen, err := gamesetting.GetVideoFullScreen()
	if err != nil {
		log.Warn().
			Err(err).
			Uint64("task_id", detail.TaskID).
			Str("entry", detail.Entry).
			Msg("Failed to read fullscreen setting, skip Alt+Enter")
		return width, height, false
	}
	if fullScreen != 1 {
		log.Debug().
			Uint64("task_id", detail.TaskID).
			Str("entry", detail.Entry).
			Uint32("video_full_screen", fullScreen).
			Msg("Game is not fullscreen, skip Alt+Enter")
		return width, height, false
	}

	log.Info().
		Uint64("task_id", detail.TaskID).
		Str("entry", detail.Entry).
		Int32("width", width).
		Int32("height", height).
		Msg("Game is fullscreen with invalid resolution, sending Alt+Enter to switch to windowed mode")

	readResolution, err := sendAltEnterWindows(controller)
	if err != nil {
		log.Warn().
			Err(err).
			Uint64("task_id", detail.TaskID).
			Str("entry", detail.Entry).
			Msg("Failed to send Alt+Enter, skip resolution recheck")
		return width, height, false
	}
	log.Debug().
		Uint64("task_id", detail.TaskID).
		Str("entry", detail.Entry).
		Msg("Alt+Enter completed, waiting for fullscreen toggle to settle")
	time.Sleep(500 * time.Millisecond)

	return recheckResolutionAfterFullscreenToggle(readResolution, detail, width, height)
}

func recheckResolutionAfterFullscreenToggle(readResolution resolutionReader, detail maa.TaskerTaskDetail, oldWidth, oldHeight int32) (int32, int32, bool) {
	width := oldWidth
	height := oldHeight

	if readResolution == nil {
		log.Warn().
			Uint64("task_id", detail.TaskID).
			Str("entry", detail.Entry).
			Msg("Resolution reader is unavailable during Alt+Enter recheck")
		return width, height, false
	}

	recheckedWidth, recheckedHeight, err := readResolution()
	if err != nil {
		log.Warn().
			Err(err).
			Uint64("task_id", detail.TaskID).
			Str("entry", detail.Entry).
			Msg("Failed to get resolution during Alt+Enter recheck")
		return width, height, false
	}

	width = recheckedWidth
	height = recheckedHeight
	aspectRatioOK, minResolutionOK, resolutionOK := isNonADBResolutionOK(width, height)
	log.Debug().
		Uint64("task_id", detail.TaskID).
		Str("entry", detail.Entry).
		Int32("width", width).
		Int32("height", height).
		Bool("aspect_ratio_ok", aspectRatioOK).
		Bool("min_resolution_ok", minResolutionOK).
		Msg("Rechecked resolution after Alt+Enter")

	if resolutionOK {
		log.Info().
			Uint64("task_id", detail.TaskID).
			Str("entry", detail.Entry).
			Int32("width", width).
			Int32("height", height).
			Msg("Resolution check passed after Alt+Enter, continuing task")
		return width, height, true
	}

	return width, height, false
}

func sendAltEnterWindows(controller *maa.Controller) (resolutionReader, error) {
	return sendAltEnterWindowsImpl(controller)
}

var sendAltEnterWindowsImpl = func(*maa.Controller) (resolutionReader, error) {
	return nil, fmt.Errorf("Alt+Enter is only supported on Windows")
}

func (c *AspectRatioChecker) stopWithWarning(tasker *maa.Tasker, controllerDisplay string, width, height int, followUpLines ...string) {
	maafocus.PrintLargeContentTrimNewline(
		i18n.RenderHTML("tasker.aspect_ratio_warning", buildWarningData(controllerDisplay, width, height, followUpLines...)),
	)
	tasker.PostStop()
}

func resolveControllerType(controller *maa.Controller) (string, string, error) {
	if controlType := normalizeControllerType(pienv.ControllerType()); controlType != "" {
		return controlType, "pi_env", nil
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

func isAtLeastTargetResolution(width, height int) bool {
	if width <= 0 || height <= 0 {
		return false
	}

	longSide := max(width, height)
	shortSide := min(width, height)
	targetLongSide := max(targetWidth, targetHeight)
	targetShortSide := min(targetWidth, targetHeight)

	return longSide >= targetLongSide && shortSide >= targetShortSide
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

func buildWarningData(controllerDisplay string, width, height int, followUpLines ...string) map[string]any {
	lines := make([]string, 0, len(followUpLines))
	for _, line := range followUpLines {
		if strings.TrimSpace(line) != "" {
			lines = append(lines, line)
		}
	}
	return map[string]any{
		"ControllerType":    controllerDisplay,
		"CurrentResolution": fmt.Sprintf("%dx%d", width, height),
		"FollowUpLines":     lines,
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
	case control.CONTROL_TYPE_WLROOTS:
		return "Wlroots"
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
	case "wlroots":
		return control.CONTROL_TYPE_WLROOTS
	default:
		return ""
	}
}
