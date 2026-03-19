package hdrcheck

import (
	_ "embed"
	"fmt"

	"github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

//go:embed warning_message.html
var hdrWarningHTML string

// HDRChecker checks if HDR is enabled on any display before task execution
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
		Msg("Checking HDR status before task execution")

	hdrEnabled, err := IsHDREnabled()
	if err != nil {
		log.Warn().Err(err).Msg("Failed to check HDR status")
		return
	}

	if hdrEnabled {
		log.Warn().Msg("HDR is enabled! This may cause issues with image recognition.")

		// Print warning message (HTML formatted for MXU display)
		fmt.Println(hdrWarningHTML)

		// Mark as warned to avoid repeated warnings
		c.warned = true
	} else {
		log.Debug().Msg("HDR check passed: HDR is not enabled")
	}
}
