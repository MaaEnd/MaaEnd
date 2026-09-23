package hdrcheck

import (
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/maafocus"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pretask/gamesetting"
	"github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

// HDRChecker checks if Endfield Auto HDR is effectively enabled before task execution.
// Display HDR must be on first; otherwise Auto HDR cannot take effect and registry is skipped.
type HDRChecker struct {
	// warned tracks whether we've already warned in this session
	// to avoid spamming the user with repeated warnings
	warned bool
}

// OnTaskerTask handles tasker task events
func (c *HDRChecker) OnTaskerTask(tasker *maa.Tasker, event maa.EventStatus, detail maa.TaskerTaskDetail) {
	// Only check on task starting
	if event != maa.EventStatusStarting {
		return
	}

	// Skip if we've already warned
	if c.warned {
		return
	}

	log.Debug().
		Uint64("task_id", detail.TaskID).
		Str("entry", detail.Entry).
		Msg("Checking display HDR status before task execution")

	displayHDR, err := IsHDREnabled()
	if err != nil {
		log.Warn().Err(err).Msg("Failed to check display HDR status")
		return
	}
	if !displayHDR {
		log.Debug().Msg("display HDR off, skip Auto HDR registry check")
		return
	}

	log.Debug().Msg("display HDR on, checking Endfield Auto HDR registry")

	autoHDR, err := gamesetting.IsAutoHDREnabled()
	if err != nil {
		log.Warn().Err(err).Msg("Failed to check Endfield Auto HDR status")
		return
	}

	if autoHDR {
		log.Warn().Msg("Endfield Auto HDR is enabled! This may cause issues with image recognition.")

		maafocus.PrintLargeContentTrimNewline(
			i18n.RenderHTML("tasker.hdr_warning", nil),
		)

		// Mark as warned to avoid repeated warnings
		c.warned = true
	} else {
		log.Debug().Msg("Auto HDR check passed: Endfield Auto HDR is not enabled")
	}
}
