package aspectratio

import (
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/control"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/gamesetting"
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

	// MaaTaskerPostStop is the synthetic entry name that fires after Tasker.PostStop().
	entryPostStop = "MaaTaskerPostStop"

	// Wait time after SetWindowPos before re-checking the new resolution (D01).
	resizeSettleDelay = 500 * time.Millisecond

	// Wait time after Alt+Enter is dispatched for the game to actually swap
	// between fullscreen and windowed mode. Most engines need a few hundred ms
	// to recreate the swap chain.
	fullscreenToggleSettleDelay = 1500 * time.Millisecond
)

// AspectRatioChecker checks if the device resolution is 16:9 before task execution.
// For Win32 controllers, if the resolution is not 16:9, it tries to silently resize
// the game window's client area to 1280x720 instead of stopping the task, and
// restores the original window rect when all tasks finish. If the game is in
// fullscreen mode, an Alt+Enter is sent first to drop into windowed mode (and
// sent again after restore to return to fullscreen).
type AspectRatioChecker struct {
	mu                sync.Mutex
	resized           bool
	fullscreenToggled bool
	targetHWnd        uintptr
	originalX         int32
	originalY         int32
	originalWidth     int32
	originalHeight    int32
}

// OnTaskerTask handles tasker task events
func (c *AspectRatioChecker) OnTaskerTask(tasker *maa.Tasker, event maa.EventStatus, detail maa.TaskerTaskDetail) {
	// PostStop entry is a synthetic task posted by Tasker::post_stop() (see
	// MaaFramework/source/MaaFramework/Tasker/Tasker.cpp). It fires only on
	// explicit `PostStop()` calls — manual stop from the client, or
	// programmatic stop via `stopWithWarning`.
	if detail.Entry == entryPostStop {
		c.handlePostStop(detail)
		return
	}

	switch event {
	case maa.EventStatusStarting:
		// Restoration only happens on explicit PostStop or process-shutdown
		// Cleanup(), so successful task completion does not trigger restore.
	case maa.EventStatusSucceeded, maa.EventStatusFailed:
		return
	default:
		return
	}

	log.Debug().
		Uint64("task_id", detail.TaskID).
		Str("entry", detail.Entry).
		Msg("Checking aspect ratio before task execution")

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
		c.handleADB(tasker, detail, controlType, controllerDisplay, width, height)
		return
	}

	log.Debug().
		Uint64("task_id", detail.TaskID).
		Str("entry", detail.Entry).
		Str("controller_name", pienv.ControllerName()).
		Str("controller_type", controlType).
		Str("requirement", "aspect_ratio").
		Str("mode", "aspect_ratio_only").
		Int32("width", width).
		Int32("height", height).
		Float64("target_ratio", targetRatio).
		Msg("Using aspect ratio check for non-ADB controller")

	if isAspectRatio16x9(int(width), int(height)) {
		log.Debug().
			Uint64("task_id", detail.TaskID).
			Str("entry", detail.Entry).
			Str("controller_name", pienv.ControllerName()).
			Str("controller_type", controlType).
			Str("requirement", "aspect_ratio").
			Int32("width", width).
			Int32("height", height).
			Str("mode", "aspect_ratio_only").
			Msg("resolution check passed")
		return
	}

	// Not 16:9. For Win32, try auto-resizing the window before giving up.
	if controlType == control.CONTROL_TYPE_WIN32 && !c.alreadyResized() {
		if c.tryResizeAndVerify(controller, detail, width, height) {
			log.Info().
				Uint64("task_id", detail.TaskID).
				Str("entry", detail.Entry).
				Msg("Window auto-resized to 16:9, continuing task")
			return
		}
		// fall through to stopWithWarning
	}

	c.warnAndStop(tasker, controllerDisplay, controlType, detail, int(width), int(height))
}

// handlePostStop restores the original window rect after all tasks finish.
// If we earlier toggled the game out of fullscreen via Alt+Enter, send Alt+Enter
// again after restoring the rect so the user lands back in fullscreen.
func (c *AspectRatioChecker) handlePostStop(detail maa.TaskerTaskDetail) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.resized && !c.fullscreenToggled {
		log.Debug().
			Uint64("task_id", detail.TaskID).
			Msg("PostStop received, no window state to restore")
		return
	}

	if c.resized && c.targetHWnd != 0 {
		hwnd := c.targetHWnd
		x, y, w, h := c.originalX, c.originalY, c.originalWidth, c.originalHeight
		if err := RestoreWindowRect(hwnd, x, y, w, h); err != nil {
			log.Warn().
				Err(err).
				Uint64("task_id", detail.TaskID).
				Uint64("hwnd", uint64(hwnd)).
				Int32("x", x).
				Int32("y", y).
				Int32("w", w).
				Int32("h", h).
				Msg("Failed to restore original window rect")
		} else {
			log.Info().
				Uint64("task_id", detail.TaskID).
				Uint64("hwnd", uint64(hwnd)).
				Int32("x", x).
				Int32("y", y).
				Int32("w", w).
				Int32("h", h).
				Msg("Restored original window rect")
		}
	}

	if c.fullscreenToggled && c.targetHWnd != 0 {
		if err := SendAltEnter(c.targetHWnd); err != nil {
			log.Warn().Err(err).Uint64("task_id", detail.TaskID).Msg("Failed to send Alt+Enter to restore fullscreen")
		} else {
			log.Info().Uint64("task_id", detail.TaskID).Msg("Sent Alt+Enter to restore fullscreen")
		}
	}

	c.resized = false
	c.fullscreenToggled = false
	c.targetHWnd = 0
	c.originalX = 0
	c.originalY = 0
	c.originalWidth = 0
	c.originalHeight = 0
}

// handleADB handles the ADB-controller exact-resolution check (original behavior).
func (c *AspectRatioChecker) handleADB(tasker *maa.Tasker, detail maa.TaskerTaskDetail, controlType, controllerDisplay string, width, height int32) {
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
}

// warnAndStop emits the standard 16:9-required warning and stops the tasker.
func (c *AspectRatioChecker) warnAndStop(tasker *maa.Tasker, controllerDisplay, controlType string, detail maa.TaskerTaskDetail, width, height int) {
	actualRatio := calculateAspectRatio(width, height)
	log.Error().
		Uint64("task_id", detail.TaskID).
		Str("entry", detail.Entry).
		Str("controller_name", pienv.ControllerName()).
		Str("controller_type", controlType).
		Str("requirement", "aspect_ratio").
		Bool("stop_task", true).
		Int("width", width).
		Int("height", height).
		Float64("actual_ratio", actualRatio).
		Float64("target_ratio", targetRatio).
		Str("mode", "aspect_ratio_only").
		Msg("resolution check failed")
	fullScreen, _ := gamesetting.GetVideoFullScreen()
	if fullScreen == 1 {
		c.stopWithWarning(tasker, controllerDisplay, width, height, i18n.T("tasker.aspect_ratio_warning.full_screen_illegal"))
	} else {
		c.stopWithWarning(tasker, controllerDisplay, width, height, i18n.T("tasker.aspect_ratio_warning.requirement_ratio"))
	}
}

func (c *AspectRatioChecker) alreadyResized() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.resized
}

// tryResizeAndVerify locates the Win32 controller's attached window via
// MaaFramework's controller-info HWND, resizes its client area to 1280x720,
// waits for the change to settle, and verifies the new resolution is 16:9.
// Returns true on success. On failure (HWND unavailable, API error, or
// post-resize resolution still not 16:9) it cleans up any partial state.
//
// If the game is currently in fullscreen mode, Alt+Enter is dispatched first
// via the controller to drop into windowed mode (resize cannot affect an
// exclusive-fullscreen window). On success the toggled state is recorded so
// handlePostStop can send Alt+Enter again to return to fullscreen.
func (c *AspectRatioChecker) tryResizeAndVerify(controller *maa.Controller, detail maa.TaskerTaskDetail, curW, curH int32) bool {
	hwnd, err := control.GetWin32HWnd(controller)
	if err != nil {
		log.Warn().
			Err(err).
			Uint64("task_id", detail.TaskID).
			Msg("Cannot resolve Win32 HWND from controller info; skip auto-resize")
		return false
	}

	fullscreenToggled := false
	if fs, ferr := gamesetting.GetVideoFullScreen(); ferr == nil && fs == 1 {
		log.Info().
			Uint64("task_id", detail.TaskID).
			Uint64("hwnd", uint64(hwnd)).
			Msg("Game is in fullscreen; sending Alt+Enter to switch to windowed mode before resize")
		if err := SendAltEnter(hwnd); err != nil {
			log.Warn().
				Err(err).
				Uint64("task_id", detail.TaskID).
				Msg("Failed to send Alt+Enter; cannot resize a fullscreen window")
			return false
		}
		fullscreenToggled = true
		time.Sleep(fullscreenToggleSettleDelay)
		controller.PostScreencap().Wait()
		// Re-read resolution after the switch; the game window may now have a
		// different size that's still not 16:9. We don't fail on that — the
		// actual resize below will normalize it.
		if newW, newH, ok := readResolutionWithRetry(controller); ok {
			curW, curH = newW, newH
			log.Debug().
				Uint64("task_id", detail.TaskID).
				Int32("post_toggle_w", curW).
				Int32("post_toggle_h", curH).
				Msg("Updated resolution after fullscreen toggle")
		}
	}

	origX, origY, origW, origH, err := ResizeClientArea(hwnd, targetWidth, targetHeight)
	if err != nil {
		log.Warn().
			Err(err).
			Uint64("task_id", detail.TaskID).
			Uint64("hwnd", uint64(hwnd)).
			Int32("cur_w", curW).
			Int32("cur_h", curH).
			Msg("Failed to resize window to 16:9")
		c.rollbackFullscreenToggle(hwnd, detail, fullscreenToggled)
		return false
	}

	log.Info().
		Uint64("task_id", detail.TaskID).
		Uint64("hwnd", uint64(hwnd)).
		Int32("orig_x", origX).
		Int32("orig_y", origY).
		Int32("orig_w", origW).
		Int32("orig_h", origH).
		Int("target_client_w", targetWidth).
		Int("target_client_h", targetHeight).
		Msg("Window resized, verifying new resolution")

	time.Sleep(resizeSettleDelay)
	controller.PostScreencap().Wait()

	newW, newH, ok := readResolutionWithRetry(controller)
	if !ok || !isAspectRatio16x9(int(newW), int(newH)) {
		log.Warn().
			Uint64("task_id", detail.TaskID).
			Int32("new_w", newW).
			Int32("new_h", newH).
			Bool("read_ok", ok).
			Msg("Resolution still not 16:9 after resize; rolling back")
		if rerr := RestoreWindowRect(hwnd, origX, origY, origW, origH); rerr != nil {
			log.Warn().Err(rerr).Msg("Rollback restore failed (best-effort)")
		}
		c.rollbackFullscreenToggle(hwnd, detail, fullscreenToggled)
		return false
	}

	c.mu.Lock()
	c.resized = true
	c.fullscreenToggled = fullscreenToggled
	c.targetHWnd = hwnd
	c.originalX = origX
	c.originalY = origY
	c.originalWidth = origW
	c.originalHeight = origH
	c.mu.Unlock()
	return true
}

// rollbackFullscreenToggle is the failure-path counterpart to the Alt+Enter
// dispatched at the start of tryResizeAndVerify: if we already left fullscreen
// but the subsequent resize/verify failed, send Alt+Enter once more so the
// user isn't left stranded in windowed mode.
func (c *AspectRatioChecker) rollbackFullscreenToggle(hwnd uintptr, detail maa.TaskerTaskDetail, toggled bool) {
	if !toggled {
		return
	}
	if err := SendAltEnter(hwnd); err != nil {
		log.Warn().Err(err).Uint64("task_id", detail.TaskID).Msg("Failed to roll back fullscreen toggle")
	} else {
		log.Info().Uint64("task_id", detail.TaskID).Msg("Rolled back fullscreen toggle")
	}
}

// readResolutionWithRetry retries up to 20 times (1s apart) until the
// controller reports a usable resolution (> 100 px on both axes).
func readResolutionWithRetry(controller *maa.Controller) (int32, int32, bool) {
	const maxRetries = 20
	var width, height int32
	var err error
	for i := range maxRetries {
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
	if controlType, err := control.GetCachedControlType(controller); err == nil {
		if normalized := normalizeControllerType(controlType); normalized != "" {
			return normalized, "cache", nil
		}
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
