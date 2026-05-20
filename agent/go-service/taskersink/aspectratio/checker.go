package aspectratio

import (
	"encoding/json"
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

	entrySceneMpSetResolutionTarget = "SceneMpSetResolutionTarget"
	nodeSceneMpResolutionSelect     = "__ScenePrivateMpSettingsResolutionSelectTarget"

	// Wait time after Alt+Enter is dispatched for the game to actually swap
	// between fullscreen and windowed mode. Most engines need a few hundred ms
	// to recreate the swap chain.
	fullscreenToggleSettleDelay = 1500 * time.Millisecond
)

// AspectRatioChecker checks if the device resolution is 16:9 before task execution.
// For Win32 controllers, fullscreen mode is switched to windowed mode via Alt+Enter
// before re-checking the aspect ratio.
type AspectRatioChecker struct {
	mu                     sync.Mutex
	fullscreenToggled      bool
	targetHWnd             uintptr
	pendingSet720TaskID    uint64
	adjustingResolution    bool
	restoringResolution    bool
	adjustedTo720          bool
	adjustedTaskID         uint64
	originalResolutionText string
	originalWidth          int
	originalHeight         int
}

var _ maa.TaskerEventSink = &AspectRatioChecker{}
var _ maa.ContextEventSink = &AspectRatioChecker{}

// OnTaskerTask handles tasker task events
func (c *AspectRatioChecker) OnTaskerTask(tasker *maa.Tasker, event maa.EventStatus, detail maa.TaskerTaskDetail) {
	// PostStop entry is a synthetic task posted by Tasker::post_stop() (see
	// MaaFramework/source/MaaFramework/Tasker/Tasker.cpp). It fires only on
	// explicit `PostStop()` calls — manual stop from the client, or
	// programmatic stop via `stopWithWarning`.
	if detail.Entry == entryPostStop {
		c.handlePostStop(tasker, detail, true)
		return
	}

	if c.isAdjustingResolution() {
		return
	}

	switch event {
	case maa.EventStatusStarting:
	case maa.EventStatusSucceeded, maa.EventStatusFailed:
		if c.shouldRestoreAfterTask(detail.TaskID) {
			log.Warn().
				Uint64("task_id", detail.TaskID).
				Str("entry", detail.Entry).
				Msg("Task ended before original resolution was restored")
			return
		}
		c.restoreFullscreen(detail)
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

	if controlType == control.CONTROL_TYPE_WIN32 {
		if !c.alreadyToggledFullscreen() {
			if newW, newH, ok := c.switchFullscreenToWindowedAndRead(controller, detail); ok {
				width, height = newW, newH
				if isAspectRatio16x9(int(width), int(height)) {
					log.Info().
						Uint64("task_id", detail.TaskID).
						Str("entry", detail.Entry).
						Int32("width", width).
						Int32("height", height).
						Msg("Resolution check passed after switching fullscreen to windowed mode")
					return
				}
			}
		}
		c.markPendingSet720(detail.TaskID)
		log.Warn().
			Uint64("task_id", detail.TaskID).
			Str("entry", detail.Entry).
			Str("controller_name", pienv.ControllerName()).
			Str("controller_type", controlType).
			Int32("width", width).
			Int32("height", height).
			Msg("Win32 resolution check failed, scheduling SceneMp resolution adjustment")
		return
	}

	c.warnAndStop(tasker, controllerDisplay, controlType, detail, int(width), int(height))
}

// handlePostStop restores fullscreen mode after a tasker stop.
func (c *AspectRatioChecker) handlePostStop(_ *maa.Tasker, detail maa.TaskerTaskDetail, _ bool) {
	if c.hasPendingOriginalResolution() {
		log.Warn().
			Uint64("task_id", detail.TaskID).
			Msg("No active context available; skip original resolution restore")
	}
	c.restoreFullscreen(detail)
}

func (c *AspectRatioChecker) restoreFullscreen(detail maa.TaskerTaskDetail) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.fullscreenToggled || c.targetHWnd == 0 {
		log.Debug().
			Uint64("task_id", detail.TaskID).
			Str("entry", detail.Entry).
			Msg("No fullscreen state to restore")
		return
	}

	if err := SendAltEnter(c.targetHWnd); err != nil {
		log.Warn().Err(err).Uint64("task_id", detail.TaskID).Msg("Failed to send Alt+Enter to restore fullscreen")
	} else {
		log.Info().Uint64("task_id", detail.TaskID).Msg("Sent Alt+Enter to restore fullscreen")
	}

	c.fullscreenToggled = false
	c.targetHWnd = 0
}

func (c *AspectRatioChecker) restoreOriginalResolution(ctx *maa.Context, taskID uint64) {
	width, height, ok := c.beginRestoreOriginalResolution(taskID, false)
	if !ok {
		return
	}
	defer c.finishRestoreOriginalResolution()

	maafocus.Print(ctx, i18n.T("tasker.aspect_ratio.restore_start", width, height))
	taskDetail, err := ctx.RunTask(entrySceneMpSetResolutionTarget, buildResolutionOverride(width, height))
	if err != nil {
		log.Warn().
			Err(err).
			Uint64("task_id", taskID).
			Int("width", width).
			Int("height", height).
			Msg("Failed to run original resolution restore task")
		return
	}
	if taskDetail == nil || !taskDetail.Status.Success() {
		status := "unknown"
		if taskDetail != nil {
			status = taskDetail.Status.String()
		}
		log.Warn().
			Uint64("task_id", taskID).
			Str("status", status).
			Int("width", width).
			Int("height", height).
			Msg("Original resolution restore task failed")
		return
	}

	c.clearOriginalResolution()
	maafocus.Print(ctx, i18n.T("tasker.aspect_ratio.restore_done", width, height))
	log.Info().
		Uint64("task_id", taskID).
		Int("width", width).
		Int("height", height).
		Msg("Original resolution restored")
	c.restoreFullscreen(maa.TaskerTaskDetail{TaskID: taskID, Entry: entrySceneMpSetResolutionTarget})
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

func (c *AspectRatioChecker) alreadyToggledFullscreen() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.fullscreenToggled
}

func (c *AspectRatioChecker) switchFullscreenToWindowedAndRead(controller *maa.Controller, detail maa.TaskerTaskDetail) (int32, int32, bool) {
	fs, ferr := gamesetting.GetVideoFullScreen()
	if ferr != nil || fs != 1 {
		return 0, 0, false
	}

	hwnd, err := getWin32HWnd(controller)
	if err != nil {
		log.Warn().
			Err(err).
			Uint64("task_id", detail.TaskID).
			Msg("Cannot resolve Win32 HWND from controller info; skip fullscreen toggle")
		return 0, 0, false
	}

	log.Info().
		Uint64("task_id", detail.TaskID).
		Uint64("hwnd", uint64(hwnd)).
		Msg("Game is in fullscreen; sending Alt+Enter to switch to windowed mode")
	if err := SendAltEnter(hwnd); err != nil {
		log.Warn().
			Err(err).
			Uint64("task_id", detail.TaskID).
			Msg("Failed to send Alt+Enter to switch fullscreen to windowed mode")
		return 0, 0, false
	}

	c.mu.Lock()
	c.fullscreenToggled = true
	c.targetHWnd = hwnd
	c.mu.Unlock()

	time.Sleep(fullscreenToggleSettleDelay)
	controller.PostScreencap().Wait()
	newW, newH, ok := readResolutionWithRetry(controller)
	if ok {
		log.Debug().
			Uint64("task_id", detail.TaskID).
			Int32("post_toggle_w", newW).
			Int32("post_toggle_h", newH).
			Msg("Updated resolution after fullscreen toggle")
	}
	return newW, newH, ok
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

// OnNodePipelineNode runs SceneMp resolution adjustment and restore while a context is active.
func (c *AspectRatioChecker) OnNodePipelineNode(ctx *maa.Context, event maa.EventStatus, detail maa.NodePipelineNodeDetail) {
	if event != maa.EventStatusStarting || !c.claimPendingSet720(detail.TaskID) {
		return
	}

	c.setAdjustingResolution(true)
	defer c.setAdjustingResolution(false)

	maafocus.Print(ctx, i18n.T("tasker.aspect_ratio.adjust_start", targetWidth, targetHeight))
	taskDetail, err := ctx.RunTask(entrySceneMpSetResolutionTarget, buildResolutionOverride(targetWidth, targetHeight))
	if err != nil {
		log.Warn().Err(err).Uint64("task_id", detail.TaskID).Msg("Failed to run SceneMp resolution adjustment")
		return
	}
	if taskDetail == nil || !taskDetail.Status.Success() {
		status := "unknown"
		if taskDetail != nil {
			status = taskDetail.Status.String()
		}
		log.Warn().Uint64("task_id", detail.TaskID).Str("status", status).Msg("SceneMp resolution adjustment failed")
		return
	}

	c.markAdjustedTo720(detail.TaskID)
	maafocus.Print(ctx, i18n.T("tasker.aspect_ratio.adjust_done", targetWidth, targetHeight))
	log.Info().Uint64("task_id", detail.TaskID).Msg("SceneMp resolution adjusted to 1280x720")
}

func (c *AspectRatioChecker) OnNodeRecognitionNode(_ *maa.Context, _ maa.EventStatus, _ maa.NodeRecognitionNodeDetail) {
}

func (c *AspectRatioChecker) OnNodeActionNode(_ *maa.Context, _ maa.EventStatus, _ maa.NodeActionNodeDetail) {
}

func (c *AspectRatioChecker) OnNodeNextList(_ *maa.Context, _ maa.EventStatus, _ maa.NodeNextListDetail) {
}

func (c *AspectRatioChecker) OnNodeRecognition(_ *maa.Context, _ maa.EventStatus, _ maa.NodeRecognitionDetail) {
}

func (c *AspectRatioChecker) OnNodeAction(ctx *maa.Context, event maa.EventStatus, detail maa.NodeActionDetail) {
	if event == maa.EventStatusSucceeded && c.shouldRestoreAfterTask(detail.TaskID) && isTerminalTaskNode(detail.Name) {
		c.restoreOriginalResolution(ctx, detail.TaskID)
	}
}

func (c *AspectRatioChecker) markPendingSet720(taskID uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pendingSet720TaskID = taskID
}

func (c *AspectRatioChecker) claimPendingSet720(taskID uint64) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.pendingSet720TaskID != taskID || c.adjustingResolution || c.restoringResolution {
		return false
	}
	c.pendingSet720TaskID = 0
	return true
}

func (c *AspectRatioChecker) isAdjustingResolution() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.adjustingResolution || c.restoringResolution
}

func (c *AspectRatioChecker) setAdjustingResolution(adjusting bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.adjustingResolution = adjusting
}

func (c *AspectRatioChecker) markAdjustedTo720(taskID uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.adjustedTo720 = true
	c.adjustedTaskID = taskID
}

func (c *AspectRatioChecker) shouldRestoreAfterTask(taskID uint64) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.adjustedTo720 && c.adjustedTaskID == taskID
}

func (c *AspectRatioChecker) hasPendingOriginalResolution() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.adjustedTo720 && c.originalWidth > 0 && c.originalHeight > 0
}

func (c *AspectRatioChecker) shouldRecordOriginalResolution() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.adjustingResolution && !c.restoringResolution && c.originalResolutionText == ""
}

func (c *AspectRatioChecker) saveOriginalResolution(text string, width, height int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.adjustingResolution || c.restoringResolution || c.originalResolutionText != "" {
		return
	}
	c.originalResolutionText = text
	c.originalWidth = width
	c.originalHeight = height
}

func (c *AspectRatioChecker) beginRestoreOriginalResolution(taskID uint64, force bool) (int, int, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.adjustedTo720 || c.originalWidth <= 0 || c.originalHeight <= 0 || c.restoringResolution {
		return 0, 0, false
	}
	if !force && c.adjustedTaskID != taskID {
		return 0, 0, false
	}
	c.restoringResolution = true
	return c.originalWidth, c.originalHeight, true
}

func (c *AspectRatioChecker) finishRestoreOriginalResolution() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.restoringResolution = false
}

func (c *AspectRatioChecker) clearOriginalResolution() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.adjustedTo720 = false
	c.adjustedTaskID = 0
	c.originalResolutionText = ""
	c.originalWidth = 0
	c.originalHeight = 0
}

func isTerminalTaskNode(name string) bool {
	return strings.HasSuffix(name, "End") ||
		strings.HasSuffix(name, "Finish") ||
		strings.HasSuffix(name, "Finished")
}

func buildResolutionOverride(width, height int) map[string]any {
	return map[string]any{
		nodeSceneMpResolutionSelect: map[string]any{
			"custom_recognition_param": map[string]any{
				"roi":      []string{"WIDTH-367", "348", "269", "231"},
				"expected": buildResolutionExpected(width, height),
			},
		},
	}
}

func buildResolutionExpected(width, height int) []string {
	return []string{
		fmt.Sprintf("%d.*%d", width, height),
		fmt.Sprintf("%d×%d", width, height),
		fmt.Sprintf("%dx%d", width, height),
		fmt.Sprintf("%d\\*%d", width, height),
	}
}

func getWin32HWnd(controller *maa.Controller) (uintptr, error) {
	if controller == nil {
		return 0, fmt.Errorf("nil controller")
	}
	infoStr, err := controller.GetInfo()
	if err != nil {
		return 0, err
	}
	var info struct {
		HWnd uint64 `json:"hwnd"`
	}
	if err := json.Unmarshal([]byte(infoStr), &info); err != nil {
		return 0, err
	}
	if info.HWnd == 0 {
		return 0, fmt.Errorf("empty win32 hwnd")
	}
	return uintptr(info.HWnd), nil
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
