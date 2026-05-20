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
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const (
	targetRatio  = 16.0 / 9.0
	tolerance    = 0.02
	targetWidth  = 1280
	targetHeight = 720

	entryPostStop                   = "MaaTaskerPostStop"
	entrySceneMpSetResolution1280   = "SceneMpSetResolution1280x720"
	entrySceneMpRestoreResolution   = "SceneMpRestoreResolution"
	entrySceneMpSetResolutionTarget = "SceneMpSetResolutionTarget"
	nodeSceneMpResolutionSelect     = "__ScenePrivateMpSettingsResolutionSelectTarget"

	fullscreenToggleSettleDelay = 1500 * time.Millisecond
)

// AspectRatioChecker checks if the device resolution is 16:9 before task execution.
type AspectRatioChecker struct {
	mu                     sync.Mutex
	adjustingResolution    bool
	restoringResolution    bool
	fullscreenToggled      bool
	targetHWnd             uintptr
	originalResolutionText string
	originalWidth          int
	originalHeight         int
}

var _ maa.TaskerEventSink = &AspectRatioChecker{}
var _ maa.ContextEventSink = &AspectRatioChecker{}

// OnTaskerTask handles tasker task events.
func (c *AspectRatioChecker) OnTaskerTask(tasker *maa.Tasker, event maa.EventStatus, detail maa.TaskerTaskDetail) {
	if detail.Entry == entryPostStop {
		log.Debug().Msg("Received PostStop event, skipping aspect ratio check")
		return
	}

	if detail.Entry == entrySceneMpSetResolution1280 {
		c.handleSetResolutionTaskEvent(tasker, event, detail)
		return
	}
	if detail.Entry == entrySceneMpRestoreResolution {
		return
	}

	if event != maa.EventStatusStarting || c.isResolutionTaskRunning() {
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

	if !isAspectRatio16x9(int(width), int(height)) {
		c.warnAndStop(tasker, controllerDisplay, controlType, detail, int(width), int(height))
		return
	}

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
}

func (c *AspectRatioChecker) handleSetResolutionTaskEvent(tasker *maa.Tasker, event maa.EventStatus, detail maa.TaskerTaskDetail) {
	switch event {
	case maa.EventStatusStarting:
		c.setAdjustingResolution(true)
		controller := tasker.GetController()
		if controller == nil {
			log.Warn().Uint64("task_id", detail.TaskID).Msg("Failed to get controller for fullscreen switch")
			return
		}
		c.switchFullscreenToWindowed(controller, detail)
	case maa.EventStatusSucceeded, maa.EventStatusFailed:
		c.setAdjustingResolution(false)
	}
}

func (c *AspectRatioChecker) restoreOriginalResolution(ctx *maa.Context) bool {
	width, height, ok := c.beginRestoreOriginalResolution()
	if !ok {
		maafocus.Print(ctx, i18n.T("tasker.aspect_ratio.restore_skip"))
		log.Info().Msg("No original resolution recorded, skipping restore")
		c.restoreFullscreen(maa.TaskerTaskDetail{Entry: entrySceneMpRestoreResolution})
		return true
	}
	defer c.finishRestoreOriginalResolution()

	maafocus.Print(ctx, i18n.T("tasker.aspect_ratio.restore_start", width, height))
	taskDetail, err := ctx.RunTask(entrySceneMpSetResolutionTarget, buildResolutionOverride(width, height))
	if err != nil {
		log.Warn().Err(err).Int("width", width).Int("height", height).Msg("Failed to run original resolution restore task")
		return false
	}
	if taskDetail == nil || !taskDetail.Status.Success() {
		status := "unknown"
		if taskDetail != nil {
			status = taskDetail.Status.String()
		}
		log.Warn().Str("status", status).Int("width", width).Int("height", height).Msg("Original resolution restore task failed")
		return false
	}

	c.clearOriginalResolution()
	maafocus.Print(ctx, i18n.T("tasker.aspect_ratio.restore_done", width, height))
	log.Info().Int("width", width).Int("height", height).Msg("Original resolution restored")
	c.restoreFullscreen(maa.TaskerTaskDetail{Entry: entrySceneMpRestoreResolution})
	return true
}

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
	queueHint := i18n.T("tasker.aspect_ratio_warning.queue_hint")
	if fullScreen == 1 {
		c.stopWithWarning(tasker, controllerDisplay, width, height, i18n.T("tasker.aspect_ratio_warning.full_screen_illegal"), queueHint)
	} else {
		c.stopWithWarning(tasker, controllerDisplay, width, height, i18n.T("tasker.aspect_ratio_warning.requirement_ratio"), queueHint)
	}
}

func (c *AspectRatioChecker) switchFullscreenToWindowed(controller *maa.Controller, detail maa.TaskerTaskDetail) {
	fs, ferr := gamesetting.GetVideoFullScreen()
	if ferr != nil || fs != 1 {
		return
	}

	hwnd, err := getWin32HWnd(controller)
	if err != nil {
		log.Warn().Err(err).Uint64("task_id", detail.TaskID).Msg("Cannot resolve Win32 HWND from controller info; skip fullscreen toggle")
		return
	}

	log.Info().Uint64("task_id", detail.TaskID).Uint64("hwnd", uint64(hwnd)).Msg("Game is in fullscreen; sending Alt+Enter to switch to windowed mode")
	if err := SendAltEnter(hwnd); err != nil {
		log.Warn().Err(err).Uint64("task_id", detail.TaskID).Msg("Failed to send Alt+Enter to switch fullscreen to windowed mode")
		return
	}

	c.mu.Lock()
	c.fullscreenToggled = true
	c.targetHWnd = hwnd
	c.mu.Unlock()

	time.Sleep(fullscreenToggleSettleDelay)
	controller.PostScreencap().Wait()
}

func (c *AspectRatioChecker) restoreFullscreen(detail maa.TaskerTaskDetail) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.fullscreenToggled || c.targetHWnd == 0 {
		log.Debug().Str("entry", detail.Entry).Msg("No fullscreen state to restore")
		return
	}

	if err := SendAltEnter(c.targetHWnd); err != nil {
		log.Warn().Err(err).Str("entry", detail.Entry).Msg("Failed to send Alt+Enter to restore fullscreen")
	} else {
		log.Info().Str("entry", detail.Entry).Msg("Sent Alt+Enter to restore fullscreen")
	}
	c.fullscreenToggled = false
	c.targetHWnd = 0
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

func (c *AspectRatioChecker) OnNodePipelineNode(_ *maa.Context, _ maa.EventStatus, _ maa.NodePipelineNodeDetail) {
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

func (c *AspectRatioChecker) isResolutionTaskRunning() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.adjustingResolution || c.restoringResolution
}

func (c *AspectRatioChecker) setAdjustingResolution(adjusting bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.adjustingResolution = adjusting
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

func (c *AspectRatioChecker) beginRestoreOriginalResolution() (int, int, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.originalWidth <= 0 || c.originalHeight <= 0 || c.restoringResolution {
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
	c.originalResolutionText = ""
	c.originalWidth = 0
	c.originalHeight = 0
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

func isAspectRatio16x9(width, height int) bool {
	if width <= 0 || height <= 0 {
		return false
	}

	ratio := calculateAspectRatio(width, height)
	return math.Abs(ratio-targetRatio) <= targetRatio*tolerance
}

func calculateAspectRatio(width, height int) float64 {
	w := float64(width)
	h := float64(height)

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
