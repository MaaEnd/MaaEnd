package hdrcheck

import (
	"fmt"

	"github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

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
		fmt.Println(`<span style="color: #ff9800; font-size: 1.6em; font-weight: 900;">⚠️ 警告：检测到 HDR 已开启</span>` +
			`<br/><span style="color: #faad14; font-size: 1.3em; font-weight: bold;">🖥️ HDR 可能导致截图颜色异常，影响图像识别准确性</span>` +
			`<br/><span style="font-size: 1.2em; font-weight: bold;">💡 建议：</span>` +
			`<br/><span style="color: #00bfff; font-size: 1.2em;">  • Windows 设置 → 显示 → 关闭 "使用 HDR"</span>` +
			`<br/><span style="color: #00bfff; font-size: 1.2em;">  • 或在图形驱动设置中关闭 HDR</span>` +
			`<br/><br/><span style="font-size: 1.1em; color: #888;">ℹ️ 任务将继续执行，但可能出现识别问题</span>`)

		// Mark as warned to avoid repeated warnings
		c.warned = true
	} else {
		log.Debug().Msg("HDR check passed: HDR is not enabled")
	}
}
