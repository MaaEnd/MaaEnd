package achievement

import (
	"time"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

func Register() {
	// Load achievement rules from JSON file (cross-project compatible path).
	if err := loadRules(rulesFilePath); err != nil {
		log.Warn().
			Err(err).
			Str("component", "Achievement").
			Str("path", rulesFilePath).
			Msg("failed to load achievement rules, using empty ruleset")
	}

	// 首次启动成就（带 DedupeKey，只生效一次）
	if _, err := recordEvent(resolveStoragePathFunc(), eventOpenMXU, 1, "first_startup_v1", time.Now()); err != nil {
		log.Warn().
			Err(err).
			Str("component", "Achievement").
			Msg("failed to record startup achievement")
	}

	maa.AgentServerRegisterCustomAction("AchievementTrackerAction", &TrackerAction{})
	maa.AgentServerRegisterCustomRecognition("AchievementPendingRecognition", &PendingRecognition{})
	maa.AgentServerRegisterCustomAction("AchievementConsumePendingAction", &ConsumePendingAction{})
	maa.AgentServerRegisterCustomAction("AchievementReportAction", &ReportAction{})
}
