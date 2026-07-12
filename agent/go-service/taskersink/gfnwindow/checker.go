// Package gfnwindow adjusts the GeForce NOW (GFN) native client window to the
// 1280x720 baseline client area before task execution.
//
// The GFN native client streams the game inside a borderless CEF window
// (see docs/zh_cn/developers/gfn-app-support-prd.md). When the local window is
// not at the 720p baseline, fixed-ROI recognition fails systematically, so this
// sink resizes the window on task start. Resize failures degrade gracefully:
// the task keeps running and the user is guided to lock the streaming
// resolution to 1280x720 in the GFN client settings.
package gfnwindow

import (
	"errors"
	"fmt"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/maafocus"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/pienv"
	"github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const (
	// gfnControllerName matches the "name" of the GFN controller entry in
	// assets/interface.json.
	gfnControllerName = "GFN-App"

	// Baseline client area required by all pipeline ROIs.
	targetWidth  = 1280
	targetHeight = 720
	// Tolerance (pixels) when comparing the client area against the baseline.
	sizeTolerance = 2
)

// Resize outcome reasons, mirroring the MaaNTE agent result contract.
const (
	reasonAlreadyMatched        = "already_matched"
	reasonResized               = "resized"
	reasonResizeFailed          = "resize_failed"
	reasonWorkAreaTooSmall      = "work_area_too_small"
	reasonClientSizeUnavailable = "client_size_unavailable"
)

// errUnsupportedPlatform is returned by the non-Windows resize stub.
var errUnsupportedPlatform = errors.New("GFN window adjustment is only supported on Windows")

// resizeResult reports what the resize attempt observed and did.
type resizeResult struct {
	Reason       string
	BeforeWidth  int32
	BeforeHeight int32
	AfterWidth   int32
	AfterHeight  int32
}

// GFNWindowChecker detects the GFN-App controller and ensures the stream
// window client area matches the 1280x720 baseline before task execution.
type GFNWindowChecker struct {
	// hintPrinted tracks whether the stream-resolution hint has been shown
	// in this session, to avoid spamming the user on every task start.
	hintPrinted bool
	// warnPrinted tracks whether the resize-failure warning has been shown
	// in this session.
	warnPrinted bool
}

// OnTaskerTask handles tasker task events.
func (c *GFNWindowChecker) OnTaskerTask(tasker *maa.Tasker, event maa.EventStatus, detail maa.TaskerTaskDetail) {
	if event != maa.EventStatusStarting {
		return
	}

	if detail.Entry == "MaaTaskerPostStop" {
		// Ignore post-stop events to avoid redundant checks
		log.Debug().Msg("Received PostStop event, skipping GFN window check")
		return
	}

	if pienv.ControllerName() != gfnControllerName {
		return
	}

	log.Info().
		Uint64("task_id", detail.TaskID).
		Str("entry", detail.Entry).
		Str("controller_name", gfnControllerName).
		Msg("GFN-App controller detected: cloud streaming session, foreground mode only")

	controller := tasker.GetController()
	if controller == nil {
		log.Error().
			Uint64("task_id", detail.TaskID).
			Str("entry", detail.Entry).
			Msg("Failed to get controller from tasker, skipping GFN window check")
		return
	}

	result, err := ensureGFNWindowResolution(controller)
	if err != nil {
		if errors.Is(err, errUnsupportedPlatform) {
			log.Debug().
				Uint64("task_id", detail.TaskID).
				Str("entry", detail.Entry).
				Msg("Skip GFN window adjustment outside Windows")
			return
		}
		log.Warn().
			Err(err).
			Uint64("task_id", detail.TaskID).
			Str("entry", detail.Entry).
			Msg("Failed to adjust GFN window resolution, task continues without resize")
		c.printResizeFailed("-")
		return
	}

	switch result.Reason {
	case reasonAlreadyMatched:
		log.Debug().
			Uint64("task_id", detail.TaskID).
			Str("entry", detail.Entry).
			Int32("width", result.BeforeWidth).
			Int32("height", result.BeforeHeight).
			Msg("GFN window client area already matches the 720p baseline")
	case reasonResized:
		log.Info().
			Uint64("task_id", detail.TaskID).
			Str("entry", detail.Entry).
			Int32("before_width", result.BeforeWidth).
			Int32("before_height", result.BeforeHeight).
			Int32("after_width", result.AfterWidth).
			Int32("after_height", result.AfterHeight).
			Msg("GFN window client area resized to the 720p baseline")
		// The controller caches the resolution of its last screencap; without a
		// fresh capture the aspect ratio check that runs after this sink would
		// still read the pre-resize size and force-stop the task.
		refreshControllerResolution(controller, detail)
		// The GFN stream render resolution is fixed when the session starts:
		// resizing the local window does not re-render the cloud output, so an
		// already-streaming session keeps its original resolution and fixed-ROI
		// recognition may still fail until the session is restarted.
		c.printStreamHint()
	default:
		log.Warn().
			Uint64("task_id", detail.TaskID).
			Str("entry", detail.Entry).
			Str("reason", result.Reason).
			Int32("before_width", result.BeforeWidth).
			Int32("before_height", result.BeforeHeight).
			Int32("after_width", result.AfterWidth).
			Int32("after_height", result.AfterHeight).
			Msg("GFN window resize did not take effect, task continues at current resolution")
		c.printResizeFailed(formatResolution(result.AfterWidth, result.AfterHeight))
	}
}

// refreshControllerResolution forces a screencap so the controller's cached
// resolution reflects the freshly resized client area.
func refreshControllerResolution(controller *maa.Controller, detail maa.TaskerTaskDetail) {
	if job := controller.PostScreencap(); job != nil {
		job.Wait()
	}
	width, height, err := controller.GetResolution()
	if err != nil {
		log.Warn().
			Err(err).
			Uint64("task_id", detail.TaskID).
			Str("entry", detail.Entry).
			Msg("Failed to read controller resolution after GFN window resize")
		return
	}
	log.Debug().
		Uint64("task_id", detail.TaskID).
		Str("entry", detail.Entry).
		Int32("width", width).
		Int32("height", height).
		Msg("Controller resolution refreshed after GFN window resize")
}

func (c *GFNWindowChecker) printStreamHint() {
	if c.hintPrinted {
		return
	}
	maafocus.PrintLargeContentTrimNewline(
		i18n.RenderHTML("tasker.gfn_stream_hint", nil),
	)
	c.hintPrinted = true
}

func (c *GFNWindowChecker) printResizeFailed(currentResolution string) {
	if c.warnPrinted {
		return
	}
	maafocus.PrintLargeContentTrimNewline(
		i18n.RenderHTML("tasker.gfn_resize_failed", map[string]any{
			"CurrentResolution": currentResolution,
		}),
	)
	c.warnPrinted = true
}

func formatResolution(width, height int32) string {
	if width <= 0 || height <= 0 {
		return "-"
	}
	return fmt.Sprintf("%dx%d", width, height)
}
