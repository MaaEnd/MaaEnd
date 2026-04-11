package aspectratio

import (
	"fmt"
	"math"
	"strings"
	"sync"
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
	tolerance     = 0.02
	targetWidth   = 1280
	targetHeight  = 720
	focusNodeName = "GoServiceAspectRatioWarningFocus"
)

// AspectRatioChecker checks if the device resolution is 16:9 before task execution
type AspectRatioChecker struct {
	mu           sync.Mutex
	checkedTasks map[uint64]struct{}
}

func (c *AspectRatioChecker) OnTaskerTask(tasker *maa.Tasker, event maa.EventStatus, detail maa.TaskerTaskDetail) {
	_ = tasker
	if event == maa.EventStatusStarting {
		return
	}
	c.forgetTask(detail.TaskID)
}

func (c *AspectRatioChecker) rememberTask(taskID uint64) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.checkedTasks == nil {
		c.checkedTasks = make(map[uint64]struct{})
	}
	if _, ok := c.checkedTasks[taskID]; ok {
		return false
	}
	c.checkedTasks[taskID] = struct{}{}
	return true
}

func (c *AspectRatioChecker) forgetTask(taskID uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.checkedTasks != nil {
		delete(c.checkedTasks, taskID)
	}
}

func (c *AspectRatioChecker) OnNodePipelineNode(ctx *maa.Context, event maa.EventStatus, detail maa.NodePipelineNodeDetail) {
	if event != maa.EventStatusStarting || detail.Name == focusNodeName {
		return
	}

	if !c.rememberTask(detail.TaskID) {
		return
	}

	tasker := ctx.GetTasker()
	if tasker == nil {
		log.Error().Uint64("task_id", detail.TaskID).Msg("failed to get tasker from context")
		return
	}

	controller := tasker.GetController()
	if controller == nil {
		log.Error().Uint64("task_id", detail.TaskID).Msg("failed to get controller from context tasker")
		return
	}

	width, height, err := getResolution(controller)
	if err != nil {
		log.Error().Err(err).Uint64("task_id", detail.TaskID).Msg("failed to get resolution")
		return
	}

	controllerType, _, _ := resolveControllerType(controller)
	controllerDisplay := displayController(pienv.ControllerName(), controllerType)
	requirement, ok := resolutionRequirement(controllerType, int(width), int(height))
	if ok {
		log.Debug().
			Uint64("task_id", detail.TaskID).
			Str("node", detail.Name).
			Str("controller_type", controllerType).
			Int32("width", width).
			Int32("height", height).
			Msg("resolution check passed")
		return
	}

	content := i18n.RenderHTML("tasker.aspect_ratio_warning", buildWarningData(controllerDisplay, int(width), int(height), requirement))

	log.Debug().
		Uint64("task_id", detail.TaskID).
		Str("node", detail.Name).
		Str("controller_type", controllerType).
		Str("requirement", requirement).
		Int32("width", width).
		Int32("height", height).
		Str("focus_node", focusNodeName).
		Msg("running pipeline focus warning for resolution check failure")

	if _, err := ctx.RunTask(focusNodeName, buildFocusNodeOverride(content)); err != nil {
		log.Warn().
			Err(err).
			Uint64("task_id", detail.TaskID).
			Str("focus_node", focusNodeName).
			Msg("failed to run pipeline focus warning node")
	}

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

func getResolution(controller *maa.Controller) (int32, int32, error) {
	const maxRetries = 20
	for i := range maxRetries {
		width, height, err := controller.GetResolution()
		if err != nil {
			return 0, 0, err
		}
		if width > 100 && height > 100 {
			return width, height, nil
		}
		log.Debug().
			Int32("width", width).
			Int32("height", height).
			Int("attempt", i+1).
			Msg("resolution too small, retrying")
		time.Sleep(time.Second)
		controller.PostScreencap().Wait()
	}
	return 0, 0, fmt.Errorf("resolution still too small after retries")
}

func resolutionRequirement(controllerType string, width, height int) (string, bool) {
	if controllerType == control.CONTROL_TYPE_ADB {
		requirement := exactResolutionRequirement()
		return requirement, width == targetWidth && height == targetHeight
	}
	requirement := aspectRatioRequirement()
	return requirement, isAspectRatio16x9(width, height)
}

func buildFocusNodeOverride(content string) map[string]any {
	return map[string]any{
		focusNodeName: map[string]any{
			"focus": map[string]any{
				maa.EventNodeAction.Starting(): map[string]any{
					"content": content,
					"display": []string{
						"log",
						"modal",
					},
				},
			},
		},
	}
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
